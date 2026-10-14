package docs_test

import (
	"strings"
	"testing"
)

func TestTeamEnrollmentReplacesPublicRegistrationAndSharedSpaces(t *testing.T) {
	for _, tt := range swaggerDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			var spec struct {
				Paths map[string]any `json:"paths" yaml:"paths"`
			}
			if err := tt.parse(tt.loadSpec(t), &spec); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/me/tenant-join-request", "/tenants/{id}/join-requests", "/tenants/{id}/join-requests/{request_id}/review", "/auth/feishu/start", "/auth/feishu/callback"} {
				if _, ok := spec.Paths[path]; !ok {
					t.Errorf("missing team authentication path %s", path)
				}
			}
			for path := range spec.Paths {
				if strings.HasPrefix(path, "/organizations") || strings.HasPrefix(path, "/shared-") || strings.Contains(path, "/shares") || strings.HasPrefix(path, "/auth/oidc/") || path == "/auth/login" || path == "/auth/register" || path == "/auth/register-by-invite" || path == "/auth/invitations/lookup" || path == "/auth/auto-setup" {
					t.Errorf("removed path still documented: %s", path)
				}
			}
		})
	}
}
