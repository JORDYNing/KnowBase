import { get, post, put } from '@/utils/request'
import type { TenantRole } from './members'

export type JoinRequestRole = Exclude<TenantRole, 'owner'>
export type JoinRequestStatus = 'pending' | 'approved' | 'rejected'
export interface TenantJoinRequest {
  id: number
  tenant_id: number
  user_id: string
  source: string
  status: JoinRequestStatus
  role?: TenantRole
  review_message?: string
  reviewed_at?: string
  created_at: string
  updated_at: string
  version: number
  username?: string
  email?: string
  avatar?: string
}
export interface MyTenantJoinRequest {
  request: TenantJoinRequest | null
  tenant: { id: number; name: string } | null
  membership: { tenant_id: number; role: TenantRole } | null
  target_tenant_id: number
  target_available: boolean
  can_resubmit: boolean
  access_revoked: boolean
}
interface Response<T> { success: boolean; data?: T; message?: string }
export function getMyTenantJoinRequest() {
  return get<Response<MyTenantJoinRequest>>('/api/v1/me/tenant-join-request')
}
export function submitTenantJoinRequest(version?: number) {
  return post<Response<MyTenantJoinRequest>>('/api/v1/me/tenant-join-request', version === undefined ? {} : { version })
}
export function listTenantJoinRequests(tenantId: number, params: { status?: JoinRequestStatus; page: number; page_size: number }) {
  return get<Response<{ requests: TenantJoinRequest[]; total: number; page: number; page_size: number }>>(
    `/api/v1/tenants/${tenantId}/join-requests`, { params },
  )
}
export function reviewTenantJoinRequest(tenantId: number, requestId: number, review: {
  approved: boolean; role?: JoinRequestRole; message?: string; version: number
}) {
  return put<Response<TenantJoinRequest>>(`/api/v1/tenants/${tenantId}/join-requests/${requestId}/review`, review)
}
