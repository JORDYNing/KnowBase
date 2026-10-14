<template>
  <section v-if="canReview" class="join-requests" aria-labelledby="join-requests-title">
    <div class="requests-header">
      <div><h3 id="join-requests-title">{{ $t('teamAccess.review.title') }} <span class="request-count">{{ total }}</span></h3>
        <p>{{ $t('teamAccess.review.description') }}</p></div>
      <div class="requests-controls">
        <t-select v-model="status" :aria-label="$t('teamAccess.review.status')" style="width:140px" @change="resetAndLoad">
          <t-option v-for="item in statuses" :key="item" :value="item" :label="$t(`teamAccess.review.statuses.${item}`)" />
        </t-select>
        <t-button variant="outline" :loading="loading" @click="loadRequests">{{ $t('teamAccess.refresh') }}</t-button>
      </div>
    </div>
    <t-alert v-if="error" theme="error" :message="error" role="alert" />
    <t-table row-key="id" :data="requests" :columns="columns" :loading="loading" :empty="$t('teamAccess.review.empty')">
      <template #applicant="{ row }"><div class="applicant"><span>{{ row.username }}</span><span class="email">{{ row.email }}</span></div></template>
      <template #created_at="{ row }">{{ formatTime(row.created_at) }}</template>
      <template #status="{ row }">{{ $t(`teamAccess.review.statuses.${row.status}`) }}</template>
      <template #actions="{ row }">
        <div v-if="row.status === 'pending'" class="request-actions">
          <t-button theme="primary" size="small" variant="text" @click="openReview(row, true)">{{ $t('teamAccess.review.approve') }}</t-button>
          <t-button theme="danger" size="small" variant="text" @click="openReview(row, false)">{{ $t('teamAccess.review.reject') }}</t-button>
        </div>
        <span v-else>{{ row.review_message || '—' }}</span>
      </template>
    </t-table>
    <t-pagination v-if="total > pageSize" v-model="page" :page-size="pageSize" :total="total" :show-page-size="false" @change="loadRequests" />
    <t-dialog v-model:visible="dialogVisible" :header="$t(approved ? 'teamAccess.review.approveTitle' : 'teamAccess.review.rejectTitle')"
      :confirm-btn="{ content: $t(approved ? 'teamAccess.review.approve' : 'teamAccess.review.reject'), loading: reviewing, disabled: approved && !selectedRole, theme: approved ? 'primary' : 'danger' }"
      :cancel-btn="{ disabled: reviewing }" :close-on-esc-keydown="!reviewing" :close-on-overlay-click="!reviewing" @confirm="reviewRequest">
      <p>{{ selectedRequest?.username }} · {{ selectedRequest?.email }}</p>
      <t-form label-align="top">
        <t-form-item v-if="approved" :label="$t('teamAccess.review.role')" required-mark>
          <t-select v-model="selectedRole" :aria-label="$t('teamAccess.review.role')" :options="roleOptions" :placeholder="$t('teamAccess.review.chooseRole')" :disabled="reviewing" clearable />
        </t-form-item>
        <t-form-item :label="$t('teamAccess.review.message')">
          <t-textarea v-model="reviewMessage" :aria-label="$t('teamAccess.review.message')" :placeholder="$t('teamAccess.review.messagePlaceholder')" :disabled="reviewing" :maxlength="500" :autosize="{ minRows: 3, maxRows: 6 }" />
        </t-form-item>
      </t-form>
      <p v-if="reviewError" class="review-error" role="alert">{{ reviewError }}</p>
    </t-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { listTenantJoinRequests, reviewTenantJoinRequest, type JoinRequestRole, type JoinRequestStatus, type TenantJoinRequest } from '@/api/tenant/join-requests'
import { joinApprovalRoles } from '@/views/auth/teamAccessState'

const props = defineProps<{ tenantId: number; currentRole: string }>()
const emit = defineEmits<{ reviewed: [] }>()
const { t, locale } = useI18n()
const canReview = computed(() => props.tenantId === 10000 && joinApprovalRoles(props.currentRole).length > 0)
const roleOptions = computed(() => joinApprovalRoles(props.currentRole).map(role => ({ value: role, label: t(`tenantMember.role.${role}`) })))
const statuses: JoinRequestStatus[] = ['pending', 'approved', 'rejected']
const status = ref<JoinRequestStatus>('pending')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const requests = ref<TenantJoinRequest[]>([])
const loading = ref(false)
const error = ref('')
const dialogVisible = ref(false)
const approved = ref(false)
const selectedRequest = ref<TenantJoinRequest | null>(null)
const selectedRole = ref<JoinRequestRole | ''>('')
const reviewMessage = ref('')
const reviewError = ref('')
const reviewing = ref(false)
let requestSequence = 0
let disposed = false
const columns = computed(() => [
  { colKey: 'applicant', title: t('teamAccess.review.applicant'), minWidth: 190 },
  { colKey: 'created_at', title: t('teamAccess.requestedAt'), minWidth: 160 },
  { colKey: 'status', title: t('teamAccess.review.status'), width: 110 },
  { colKey: 'actions', title: t('teamAccess.review.actions'), minWidth: 160 },
])
function formatTime(value: string) { return new Date(value).toLocaleString(locale.value) }
async function loadRequests() {
  if (!canReview.value || disposed) return
  const sequence = ++requestSequence
  loading.value = true
  error.value = ''
  try {
    const response = await listTenantJoinRequests(props.tenantId, { status: status.value, page: page.value, page_size: pageSize })
    if (disposed || sequence !== requestSequence) return
    if (!response.success || !response.data) throw new Error(response.message || t('teamAccess.loadFailed'))
    requests.value = response.data.requests
    total.value = response.data.total
    const maxPage = Math.max(1, Math.ceil(total.value / pageSize))
    if (page.value > maxPage) { page.value = maxPage; void loadRequests() }
  } catch (err: any) {
    if (!disposed && sequence === requestSequence) error.value = err?.message || t('teamAccess.loadFailed')
  } finally { if (sequence === requestSequence) loading.value = false }
}
function resetAndLoad() { page.value = 1; void loadRequests() }
function openReview(request: TenantJoinRequest, approve: boolean) {
  selectedRequest.value = { ...request }
  approved.value = approve
  selectedRole.value = ''
  reviewMessage.value = ''
  reviewError.value = ''
  dialogVisible.value = true
}
async function reviewRequest() {
  if (!selectedRequest.value || reviewing.value || !canReview.value) return
  if (approved.value && !joinApprovalRoles(props.currentRole).includes(selectedRole.value as JoinRequestRole)) {
    reviewError.value = t('teamAccess.review.chooseRole')
    return
  }
  reviewing.value = true
  reviewError.value = ''
  try {
    const response = await reviewTenantJoinRequest(props.tenantId, selectedRequest.value.id, {
      approved: approved.value,
      ...(approved.value ? { role: selectedRole.value as JoinRequestRole } : {}),
      message: reviewMessage.value.trim() || undefined,
      version: selectedRequest.value.version,
    })
    if (!response.success) throw new Error(response.message || t('teamAccess.review.failed'))
    dialogVisible.value = false
    MessagePlugin.success(t('teamAccess.review.success'))
    emit('reviewed')
    await loadRequests()
  } catch (err: any) {
    if (!disposed) reviewError.value = err?.status === 409 ? t('teamAccess.conflict') : err?.message || t('teamAccess.review.failed')
    if (err?.status === 409) { dialogVisible.value = false; await loadRequests(); error.value = t('teamAccess.conflict') }
  } finally { reviewing.value = false }
}
watch(() => [props.tenantId, props.currentRole], () => {
  requestSequence++
  requests.value = []
  total.value = 0
  dialogVisible.value = false
  resetAndLoad()
}, { immediate: true })
onUnmounted(() => { disposed = true; requestSequence++ })
</script>

<style scoped lang="less">
.join-requests { padding:20px 0 24px; border-bottom:1px solid var(--td-component-stroke); margin-bottom:24px; }
.requests-header { display:flex; align-items:center; justify-content:space-between; gap:16px; margin-bottom:16px; flex-wrap:wrap; }
h3 { font-size:var(--app-text-lg); margin:0; color:var(--td-text-color-primary); }
.requests-header p { font-size:var(--app-text-sm); margin:8px 0 0; color:var(--td-text-color-secondary); line-height:1.6; }
.request-count { margin-left:6px; font-size:var(--app-text-sm); font-weight:normal; color:var(--td-text-color-secondary); }
.requests-controls,.request-actions { display:flex; align-items:center; gap:8px; }
.applicant { display:flex; flex-direction:column; gap:4px; }
.email { font-size:var(--app-text-sm); color:var(--td-text-color-secondary); overflow-wrap:anywhere; }
.review-error { color:var(--td-error-color); }
</style>
