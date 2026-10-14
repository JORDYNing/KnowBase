package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTeamPolicyCannotBeReopenedByLegacyConfiguration(t *testing.T) {
	t.Setenv("WEKNORA_AUTH_DEFAULT_TENANT_MODE", "create_personal")
	t.Setenv("WEKNORA_TENANT_SELF_SERVICE_CREATION_ENABLED", "true")
	t.Setenv("WEKNORA_TENANT_ENABLE_RBAC", "false")
	off, on := false, true
	cfg := &Config{Auth: &AuthConfig{RegistrationMode: AuthRegistrationModeSelfServe, DefaultTenantMode: AuthDefaultTenantModeCreatePersonal}, Tenant: &TenantConfig{EnableRBAC: &off, SelfServiceCreationEnabled: &on}, OIDCAuth: &OIDCAuthConfig{Enable: true}}
	applyAuthAndTenantDefaults(cfg)
	require.Equal(t, AuthRegistrationModeInviteOnly, cfg.Auth.RegistrationMode)
	require.Equal(t, AuthDefaultTenantModeTenantless, cfg.Auth.DefaultTenantMode)
	require.True(t, cfg.Tenant.IsRBACEnforced())
	require.False(t, cfg.Tenant.IsSelfServiceCreationEnabled())
	require.False(t, cfg.OIDCAuth.Enable)
}
