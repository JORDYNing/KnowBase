import type { MyTenantJoinRequest } from '@/api/tenant/join-requests'

export type TeamAccessState = 'pending' | 'rejected' | 'ready' | 'revoked' | 'unavailable' | 'unrequested'
/** Membership is authoritative; an old approval never grants access after removal. */
export function teamAccessState(data: MyTenantJoinRequest): TeamAccessState {
  if (!data.target_available) return 'unavailable'
  if (data.membership?.tenant_id === data.target_tenant_id) return 'ready'
  if (data.access_revoked || data.request?.status === 'approved') return 'revoked'
  if (data.request?.status === 'pending') return 'pending'
  if (data.request?.status === 'rejected') return 'rejected'
  return 'unrequested'
}
export function canSubmitTeamRequest(data: MyTenantJoinRequest): boolean {
  return data.target_available && !data.access_revoked && data.can_resubmit
    && (data.request === null || data.request.status === 'rejected')
}
export function joinApprovalRoles(role: string): Array<'viewer' | 'contributor' | 'admin'> {
  if (role === 'owner') return ['viewer', 'contributor', 'admin']
  if (role === 'admin') return ['viewer', 'contributor']
  return []
}

const REMOVED_TENANT_SETTINGS = new Set([
  'tenant.self_service_creation_enabled',
  'tenant.max_owned_per_user',
])
/** Fixed team access must not be configurable back into local registration. */
export function isTeamSystemSetting(key: string): boolean {
  return !key.startsWith('oidc.')
    && (!key.startsWith('auth.') || key.startsWith('auth.feishu.'))
    && !REMOVED_TENANT_SETTINGS.has(key)
}

export function teamRouteRedirect(options: {
  authenticated: boolean; hasWorkspace: boolean; publicPage: boolean; requiresWorkspace: boolean
}): string | undefined {
  if (!options.authenticated) return options.publicPage ? undefined : '/login'
  if (options.publicPage) return options.hasWorkspace ? '/platform/knowledge-bases' : '/onboarding/workspace'
  if (!options.requiresWorkspace) return options.hasWorkspace ? '/platform/knowledge-bases' : undefined
  return options.hasWorkspace ? undefined : '/onboarding/workspace'
}
export function shouldPollTeamRequest(state: TeamAccessState | null, visible: boolean): boolean {
  return state === 'pending' && visible
}

/** Ordinary members enter only the team; platform catalog managers may enter any active workspace. */
export function hasAccessibleWorkspace(
  tenant: { id: string | number; status?: string } | null,
  memberships: Array<{ tenant_id: number }>,
  canAccessAllTenants: boolean,
): boolean {
  const id = Number(tenant?.id)
  if (!Number.isSafeInteger(id) || id <= 0 || (tenant?.status && tenant.status !== 'active')) return false
  return canAccessAllTenants || (id === 10000 && memberships.some(member => member.tenant_id === id))
}
