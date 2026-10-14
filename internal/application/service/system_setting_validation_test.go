package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func TestValidateWorkerConcurrencyMinimums(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   any
		wantErr bool
	}{
		{name: "core zero", key: "asynq.core_concurrency", value: 0, wantErr: true},
		{name: "core minimum", key: "asynq.core_concurrency", value: 1},
		{name: "postprocess minimum", key: "asynq.postprocess_concurrency", value: 1},
		{name: "enrichment minimum", key: "asynq.enrichment_concurrency", value: 1},
		{name: "maintenance minimum", key: "asynq.maintenance_concurrency", value: 1},
		{name: "shared minimum", key: "asynq.shared_concurrency", value: 1},
		{name: "wiki zero", key: "asynq.wiki_concurrency", value: 0, wantErr: true},
		{name: "wiki minimum", key: "asynq.wiki_concurrency", value: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegistryEntry(tt.key, tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

type registrationSettingRepo struct {
	interfaces.SystemSettingRepository
	row *types.SystemSetting
}

func (r *registrationSettingRepo) Get(context.Context, string) (*types.SystemSetting, error) {
	return r.row, nil
}

func (r *registrationSettingRepo) Upsert(_ context.Context, row *types.SystemSetting) error {
	r.row = row
	return nil
}

func TestTeamAuthenticationPoliciesAreFixed(t *testing.T) {
	svc := &systemSettingService{repo: &registrationSettingRepo{}, cache: make(map[string]*types.SystemSetting)}
	ctx := context.Background()
	for _, mode := range []string{"self_serve", "invite_register", "invite_only", "unknown"} {
		if _, err := svc.Update(ctx, "auth.registration_mode", mode); err == nil {
			t.Fatalf("fixed registration policy accepted edit %q", mode)
		}
	}
	if got := svc.GetString(ctx, "auth.registration_mode", "", "self_serve"); got != "invite_only" {
		t.Fatalf("registration policy=%q", got)
	}
	if _, err := svc.Update(ctx, "auth.default_tenant_mode", "create_personal"); err == nil {
		t.Fatal("personal provisioning was reopened")
	}
	if _, err := svc.Update(ctx, "tenant.self_service_creation_enabled", true); err == nil {
		t.Fatal("human space creation was reopened")
	}

}
