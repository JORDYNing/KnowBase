<template>
  <main class="workspace-onboarding">
    <section class="workspace-card" aria-labelledby="access-title" :aria-busy="loading || entering">
      <div class="workspace-mark" aria-hidden="true"><t-icon name="usergroup" size="30px" /></div>
      <h1 id="access-title">{{ $t('teamAccess.title') }}</h1>
      <p class="workspace-description">{{ $t('teamAccess.description') }}</p>
      <p v-if="authStore.user?.email" class="account-email">{{ authStore.user.email }}</p>
      <p v-if="snapshot" class="workspace-name">{{ snapshot.tenant?.name || $t('teamAccess.team') }} · ID {{ snapshot.target_tenant_id }}</p>

      <div v-if="loading && !snapshot" role="status"><t-loading size="small" :text="$t('common.loading')" /></div>
      <template v-else-if="snapshot">
        <div class="access-state" :class="`access-state--${state}`" role="status" aria-live="polite">
          <t-tag :theme="stateTheme" variant="light">{{ $t(`teamAccess.states.${state}`) }}</t-tag>
          <p>{{ $t(`teamAccess.hints.${state}`) }}</p>
        </div>
        <dl v-if="snapshot.request" class="request-details">
          <template v-if="snapshot.request.created_at">
            <dt>{{ $t('teamAccess.requestedAt') }}</dt><dd>{{ formatTime(snapshot.request.created_at) }}</dd>
          </template>
          <template v-if="snapshot.request.reviewed_at">
            <dt>{{ $t('teamAccess.reviewedAt') }}</dt><dd>{{ formatTime(snapshot.request.reviewed_at) }}</dd>
          </template>
          <template v-if="snapshot.request.review_message">
            <dt>{{ $t('teamAccess.reviewMessage') }}</dt><dd class="review-message">{{ snapshot.request.review_message }}</dd>
          </template>
        </dl>
      </template>
      <p v-if="error" class="policy-error" role="alert">{{ error }}</p>
      <div class="workspace-actions">
        <t-button v-if="state === 'ready'" theme="primary" :loading="entering" @click="enterWorkspace">{{ $t('teamAccess.enter') }}</t-button>
        <t-button v-if="canSubmit" theme="primary" :loading="submitting" :disabled="loading" @click="submitRequest">
          {{ $t(snapshot?.request ? 'teamAccess.resubmit' : 'teamAccess.apply') }}
        </t-button>
        <t-button v-if="authStore.canAccessAllTenants" variant="outline" @click="platformWorkspacesVisible = true">{{ $t('platformWorkspaces.title') }}</t-button>
        <t-button variant="outline" :loading="loading" :disabled="entering || submitting" @click="loadStatus">{{ $t('teamAccess.refresh') }}</t-button>
      </div>
      <p v-if="state === 'pending'" class="workspace-help">{{ $t('teamAccess.pollingHint') }}</p>
      <button class="logout-link" type="button" :disabled="loggingOut" @click="handleLogout">{{ $t('auth.logout') }}</button>
    </section>
    <PlatformWorkspaceManager v-if="authStore.canAccessAllTenants && platformWorkspacesVisible" v-model:visible="platformWorkspacesVisible" />
  </main>
</template>

<script setup lang="ts">
import PlatformWorkspaceManager from '@/components/PlatformWorkspaceManager.vue'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { logout as logoutApi } from '@/api/auth'
import { getMyTenantJoinRequest, submitTenantJoinRequest, type MyTenantJoinRequest } from '@/api/tenant/join-requests'
import { useAuthStore } from '@/stores/auth'
import { canSubmitTeamRequest, teamAccessState, shouldPollTeamRequest } from './teamAccessState'

const { t, locale } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const platformWorkspacesVisible = ref(false)
const snapshot = ref<MyTenantJoinRequest | null>(null)
const loading = ref(false)
const submitting = ref(false)
const entering = ref(false)
const loggingOut = ref(false)
const error = ref('')
const state = computed(() => snapshot.value ? teamAccessState(snapshot.value) : null)
const canSubmit = computed(() => !!snapshot.value && canSubmitTeamRequest(snapshot.value))
const stateTheme = computed(() => state.value === 'ready' ? 'success' : state.value === 'pending' ? 'primary' : 'warning')
let disposed = false
let autoEntryAttempted = false
let pollTimer: ReturnType<typeof setInterval> | undefined

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString(locale.value)
}
async function enterWorkspace() {
  if (entering.value || state.value !== 'ready' || disposed) return
  entering.value = true
  error.value = ''
  autoEntryAttempted = true
  try {
    if (!await authStore.enterTeamWorkspace()) throw new Error(t('teamAccess.enterFailed'))
    if (!disposed && authStore.hasValidTenant) await router.replace('/platform/knowledge-bases')
    else if (!disposed) throw new Error(t('teamAccess.enterFailed'))
  } catch (err: any) {
    if (!disposed) error.value = err?.message || t('teamAccess.enterFailed')
  } finally { entering.value = false }
}
async function loadStatus() {
  if (loading.value || submitting.value || entering.value || disposed) return
  loading.value = true
  error.value = ''
  try {
    const response = await getMyTenantJoinRequest()
    if (disposed) return
    if (!response.success || !response.data) throw new Error(response.message || t('teamAccess.loadFailed'))
    snapshot.value = response.data
    if (teamAccessState(response.data) === 'ready' && !autoEntryAttempted) await enterWorkspace()
  } catch (err: any) {
    if (!disposed) error.value = err?.message || t('teamAccess.loadFailed')
  } finally { loading.value = false }
}
async function submitRequest() {
  if (!canSubmit.value || submitting.value || !snapshot.value) return
  submitting.value = true
  error.value = ''
  try {
    const response = await submitTenantJoinRequest(snapshot.value.request?.version)
    if (disposed) return
    if (!response.success || !response.data) throw new Error(response.message || t('teamAccess.submitFailed'))
    snapshot.value = response.data
  } catch (err: any) {
    if (!disposed) error.value = err?.status === 409 ? t('teamAccess.conflict') : err?.message || t('teamAccess.submitFailed')
    // A concurrent review or resubmission changes the request version.
    submitting.value = false
    if (err?.status === 409) await loadStatus()
  } finally { submitting.value = false }
}
function stopPolling() {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = undefined
}
function syncPolling() {
  stopPolling()
  if (disposed || !shouldPollTeamRequest(state.value, document.visibilityState === 'visible')) return
  pollTimer = setInterval(() => { void loadStatus() }, 30_000)
}
function onVisibilityChange() {
  syncPolling()
  if (document.visibilityState === 'visible' && state.value === 'pending') void loadStatus()
}
watch(state, syncPolling)
async function handleLogout() {
  if (loggingOut.value) return
  loggingOut.value = true
  disposed = true
  stopPolling()
  try { await logoutApi() } finally {
    authStore.logout()
    await router.replace('/login')
  }
}
onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  void loadStatus()
})
onUnmounted(() => {
  disposed = true
  stopPolling()
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<style scoped lang="less">
.workspace-onboarding { min-height:100dvh; display:grid; place-items:center; padding:32px 20px; background:radial-gradient(circle at 20% 10%,color-mix(in srgb,var(--td-brand-color) 12%,transparent),transparent 38%),var(--td-bg-color-page); }
.workspace-card { width:min(560px,100%); box-sizing:border-box; padding:40px; border:1px solid var(--td-component-stroke); border-radius:20px; background:var(--td-bg-color-container); box-shadow:var(--td-shadow-2); text-align:center; }
.workspace-mark { width:64px; height:64px; margin:0 auto 22px; display:grid; place-items:center; border-radius:18px; color:var(--td-brand-color); background:var(--td-brand-color-light); }
h1 { margin:0; color:var(--td-text-color-primary); font-size:var(--app-text-4xl); }
.workspace-description,.account-email,.workspace-help { color:var(--td-text-color-secondary); line-height:1.7; }
.account-email { overflow-wrap:anywhere; }
.workspace-name { font-weight:600; color:var(--td-text-color-primary); }
.access-state { margin:24px 0; padding:18px; border-radius:12px; background:var(--td-bg-color-secondarycontainer); }
.access-state p { margin:12px 0 0; line-height:1.7; color:var(--td-text-color-secondary); }
.request-details { display:grid; grid-template-columns:auto 1fr; text-align:left; gap:10px 16px; font-size:var(--app-text-md); }
.request-details dt { color:var(--td-text-color-secondary); }
.request-details dd { margin:0; color:var(--td-text-color-primary); overflow-wrap:anywhere; }
.review-message { white-space:pre-wrap; }
.policy-error { color:var(--td-error-color); line-height:1.6; }
.workspace-actions { display:flex; justify-content:center; gap:12px; flex-wrap:wrap; margin-top:24px; }
.workspace-help { font-size:var(--app-text-sm); }
.logout-link { border:0; margin-top:22px; padding:8px 12px; color:var(--td-text-color-secondary); background:transparent; cursor:pointer; }
.logout-link:focus-visible { outline:2px solid var(--td-brand-color); outline-offset:2px; }
@media(max-width:560px) { .workspace-card { padding:28px 22px; } .request-details { grid-template-columns:1fr; gap:6px; } .request-details dd { margin-bottom:6px; } }
</style>
