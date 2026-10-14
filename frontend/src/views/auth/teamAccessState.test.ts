import assert from 'node:assert/strict'
import test from 'node:test'
import { hasAccessibleWorkspace, canSubmitTeamRequest, isTeamSystemSetting, joinApprovalRoles, shouldPollTeamRequest, teamAccessState, teamRouteRedirect } from './teamAccessState.ts'
import type { MyTenantJoinRequest } from '@/api/tenant/join-requests'

const pending: MyTenantJoinRequest = {
  target_tenant_id: 10000, target_available: true, access_revoked: false, can_resubmit: false,
  tenant: { id: 10000, name: 'Team' }, membership: null,
  request: { id: 1, tenant_id: 10000, user_id: 'u1', source: 'feishu', status: 'pending', created_at: '', updated_at: '', version: 1 },
}
test('only active membership in the configured team grants access', () => {
  assert.equal(teamAccessState(pending), 'pending')
  assert.equal(teamAccessState({ ...pending, membership: { tenant_id: 10000, role: 'viewer' } }), 'ready')
  assert.equal(teamAccessState({ ...pending, membership: { tenant_id: 10001, role: 'owner' } }), 'pending')
  assert.equal(teamAccessState({ ...pending, request: { ...pending.request!, status: 'approved' } }), 'revoked')
  assert.equal(teamAccessState({ ...pending, target_available: false, membership: { tenant_id: 10000, role: 'owner' } }), 'unavailable')
})
test('rejected requests require explicit resubmission permission; old approvals cannot rejoin', () => {
  const rejected = { ...pending, can_resubmit: true, request: { ...pending.request!, status: 'rejected' as const } }
  assert.equal(teamAccessState(rejected), 'rejected')
  assert.equal(canSubmitTeamRequest(rejected), true)
  assert.equal(canSubmitTeamRequest(pending), false)
  assert.equal(canSubmitTeamRequest({ ...rejected, can_resubmit: false }), false)
  assert.equal(canSubmitTeamRequest({ ...rejected, access_revoked: true }), false)
  assert.equal(canSubmitTeamRequest({ ...rejected, target_available: false }), false)
  assert.equal(canSubmitTeamRequest({ ...rejected, request: { ...rejected.request, status: 'approved' } }), false)
})
test('reviewers can only choose roles they may assign, with no owner option', () => {
  assert.deepEqual(joinApprovalRoles('owner'), ['viewer', 'contributor', 'admin'])
  assert.deepEqual(joinApprovalRoles('admin'), ['viewer', 'contributor'])
  assert.deepEqual(joinApprovalRoles('contributor'), [])
  assert.deepEqual(joinApprovalRoles('viewer'), [])
})
test('onboarding authentication precedes its workspace exception', () => {
  assert.equal(teamRouteRedirect({ authenticated: false, hasWorkspace: false, publicPage: false, requiresWorkspace: false }), '/login')
  assert.equal(teamRouteRedirect({ authenticated: true, hasWorkspace: false, publicPage: false, requiresWorkspace: false }), undefined)
  assert.equal(teamRouteRedirect({ authenticated: true, hasWorkspace: false, publicPage: false, requiresWorkspace: true }), '/onboarding/workspace')
  assert.equal(teamRouteRedirect({ authenticated: true, hasWorkspace: true, publicPage: false, requiresWorkspace: false }), '/platform/knowledge-bases')
  assert.equal(teamRouteRedirect({ authenticated: false, hasWorkspace: false, publicPage: true, requiresWorkspace: true }), undefined)
  assert.equal(teamRouteRedirect({ authenticated: true, hasWorkspace: true, publicPage: true, requiresWorkspace: true }), '/platform/knowledge-bases')
})
test('polling only runs for pending requests on visible pages', () => {
  assert.equal(shouldPollTeamRequest('pending', true), true)
  assert.equal(shouldPollTeamRequest('pending', false), false)
  for (const state of ['ready', 'rejected', 'revoked', 'unavailable', 'unrequested', null] as const) assert.equal(shouldPollTeamRequest(state, true), false)
})
test('system settings cannot re-enable personal spaces or alternate login methods', () => {
  for (const key of ['auth.registration_mode', 'auth.default_tenant_mode', 'auth.complex_password_enabled', 'oidc.enabled', 'tenant.self_service_creation_enabled', 'tenant.max_owned_per_user']) assert.equal(isTeamSystemSetting(key), false)
  for (const key of ['tenant.default_storage_quota_gb', 'ssrf.whitelist', 'sandbox.docker_enabled', 'model.max_concurrency', 'new.setting']) assert.equal(isTeamSystemSetting(key), true)
})

test('only cross-workspace catalog managers may enter other active tenants without membership', () => {
  const team = { id: '10000', status: 'active' }
  const other = { id: '10001', status: 'active' }
  assert.equal(hasAccessibleWorkspace(team, [{ tenant_id: 10000 }], false), true)
  assert.equal(hasAccessibleWorkspace(team, [], false), false)
  assert.equal(hasAccessibleWorkspace(other, [{ tenant_id: 10001 }], false), false)
  assert.equal(hasAccessibleWorkspace(other, [], true), true)
  assert.equal(hasAccessibleWorkspace(null, [], true), false)
  assert.equal(hasAccessibleWorkspace({ ...other, status: 'suspended' }, [], true), false)
})
