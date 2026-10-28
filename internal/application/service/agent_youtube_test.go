package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type youtubeAgentTenantStub struct {
	interfaces.TenantService
	tenant *types.Tenant
	err    error
}

func (s *youtubeAgentTenantStub) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return s.tenant, s.err
}

type youtubeAgentImportStub struct {
	interfaces.YouTubeImportService
}

func TestAgentYouTubeToolRequiresAllowlistAndWorkspaceConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name                                           string
		selected, enabled, configured, available, want bool
		err                                            error
	}{
		{name: "enabled without retrieval scope", selected: true, enabled: true, configured: true, available: true, want: true},
		{name: "not selected", enabled: true, configured: true, available: true},
		{name: "disabled", selected: true, configured: true, available: true},
		{name: "no target", selected: true, enabled: true, available: true},
		{name: "no service", selected: true, enabled: true, configured: true},
		{name: "tenant lookup failed", selected: true, enabled: true, configured: true, available: true, err: errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := &types.YouTubeTranscriptConfig{Enabled: tc.enabled}
			if tc.configured {
				settings.KnowledgeBaseID = "target"
			}
			svc := &agentService{tenantService: &youtubeAgentTenantStub{tenant: &types.Tenant{ID: 7, YouTubeTranscriptConfig: settings}, err: tc.err}}
			if tc.available {
				svc.youtubeImportService = &youtubeAgentImportStub{}
			}
			cfg := &types.AgentConfig{AllowedTools: []string{tools.ToolThinking}}
			if tc.selected {
				cfg.AllowedTools = append(cfg.AllowedTools, tools.ToolYouTubeTranscriptImport)
			}
			registry := tools.NewToolRegistry()
			ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(7))
			require.NoError(t, svc.registerTools(ctx, registry, cfg, nil, nil, "session"))
			_, err := registry.GetTool(tools.ToolYouTubeTranscriptImport)
			require.Equal(t, tc.want, err == nil)
		})
	}
}
