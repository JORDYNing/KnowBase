<template>
  <t-dialog v-if="authStore.canAccessAllTenants" :visible="visible" :header="$t('platformWorkspaces.title')" width="min(760px, 94vw)"
    :footer="false" :close-btn="!busy" :close-on-esc-keydown="!busy" :close-on-overlay-click="!busy" @close="close">
    <p class="manager-description">{{ $t('platformWorkspaces.description') }}</p>
    <form class="search-controls" @submit.prevent="search">
      <t-input v-model="keyword" :aria-label="$t('platformWorkspaces.searchPlaceholder')" :placeholder="$t('platformWorkspaces.searchPlaceholder')" :disabled="busy" clearable />
      <t-button type="submit" variant="outline" :loading="loading" :disabled="busy">{{ $t('platformWorkspaces.search') }}</t-button>
      <t-button theme="primary" :disabled="busy" @click="openCreate">{{ $t('platformWorkspaces.create') }}</t-button>
    </form>
    <t-form v-if="createVisible" class="create-workspace" label-align="top" @submit="submitCreate">
      <t-form-item :label="$t('platformWorkspaces.name')" required-mark>
        <t-input v-model="name" :aria-label="$t('platformWorkspaces.name')" :maxlength="100" :disabled="busy" autofocus />
      </t-form-item>
      <t-form-item :label="$t('platformWorkspaces.workspaceDescription')">
        <t-textarea v-model="description" :aria-label="$t('platformWorkspaces.workspaceDescription')" :maxlength="500" :disabled="busy" :autosize="{ minRows: 2, maxRows: 4 }" />
      </t-form-item>
      <div class="create-actions">
        <t-button type="submit" theme="primary" :loading="creating" :disabled="!name.trim() || switchingId !== null">{{ $t('platformWorkspaces.create') }}</t-button>
        <t-button variant="text" :disabled="busy" @click="createVisible = false">{{ $t('common.cancel') }}</t-button>
      </div>
    </t-form>
    <p v-if="error" class="manager-error" role="alert">{{ error }}</p>
    <t-table row-key="id" :data="tenants" :columns="columns" :loading="loading" :empty="$t('platformWorkspaces.empty')">
      <template #name="{ row }"><div class="workspace-name">{{ row.name }}</div><div class="workspace-description">{{ row.description }}</div></template>
      <template #actions="{ row }">
        <t-tag v-if="Number(authStore.tenant?.id) === row.id" theme="primary" variant="light">{{ $t('platformWorkspaces.current') }}</t-tag>
        <t-button v-else size="small" variant="outline" :loading="switchingId === row.id" :disabled="busy || (!!row.status && row.status !== 'active')" @click="switchWorkspace(row.id)">{{ $t('platformWorkspaces.switch') }}</t-button>
      </template>
    </t-table>
    <t-pagination v-if="total > pageSize" v-model="page" :page-size="pageSize" :total="total" :show-page-size="false" :disabled="busy" @change="loadTenants" />
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { createTenant, searchTenants, type TenantInfo } from '@/api/tenant'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [value: boolean] }>()
const authStore = useAuthStore()
const { t } = useI18n()
const keyword = ref('')
const page = ref(1)
const pageSize = 10
const total = ref(0)
const tenants = ref<TenantInfo[]>([])
const loading = ref(false)
const creating = ref(false)
const switchingId = ref<number | null>(null)
const busy = computed(() => creating.value || switchingId.value !== null)
const createVisible = ref(false)
const name = ref('')
const description = ref('')
const error = ref('')
let sequence = 0
let disposed = false
const columns = computed(() => [
  { colKey: 'id', title: 'ID', width: 90 },
  { colKey: 'name', title: t('platformWorkspaces.name'), minWidth: 180 },
  { colKey: 'actions', title: t('platformWorkspaces.actions'), width: 130 },
])
function close() { if (!busy.value) emit('update:visible', false) }
function openCreate() {
  if (!authStore.canAccessAllTenants || busy.value) return
  name.value = ''; description.value = ''; error.value = ''; createVisible.value = true
}
async function loadTenants() {
  if (!authStore.canAccessAllTenants || !props.visible || disposed || busy.value) return
  const requestId = ++sequence
  loading.value = true; error.value = ''
  try {
    const response = await searchTenants({ keyword: keyword.value.trim(), page: page.value, page_size: pageSize })
    if (disposed || requestId !== sequence || !authStore.canAccessAllTenants) return
    if (!response.success || !response.data) throw new Error(response.message || t('platformWorkspaces.loadFailed'))
    tenants.value = response.data.items; total.value = response.data.total
  } catch (err: any) {
    if (!disposed && requestId === sequence) error.value = err?.message || t('platformWorkspaces.loadFailed')
  } finally { if (requestId === sequence) loading.value = false }
}
function search() { page.value = 1; void loadTenants() }
async function submitCreate() {
  if (!authStore.canAccessAllTenants || busy.value || !name.value.trim() || disposed) return
  creating.value = true; error.value = ''
  try {
    const response = await createTenant({ name: name.value.trim(), description: description.value.trim() })
    if (disposed || !authStore.canAccessAllTenants) return
    if (!response.success || !response.data) throw new Error(response.message || t('platformWorkspaces.createFailed'))
    createVisible.value = false
    MessagePlugin.success(t('platformWorkspaces.created'))
    keyword.value = ''; page.value = 1
    creating.value = false
    await loadTenants()
  } catch (err: any) {
    if (!disposed) error.value = err?.message || t('platformWorkspaces.createFailed')
  } finally { creating.value = false }
}
async function switchWorkspace(tenantId: number) {
  if (!authStore.canAccessAllTenants || busy.value || disposed || !Number.isSafeInteger(tenantId) || tenantId <= 0) return
  switchingId.value = tenantId; error.value = ''
  try {
    if (!await authStore.switchPlatformWorkspace(tenantId)) throw new Error(t('platformWorkspaces.switchFailed'))
    if (!disposed) window.location.assign('/platform/knowledge-bases')
  } catch (err: any) {
    if (!disposed) error.value = err?.message || t('platformWorkspaces.switchFailed')
  } finally { switchingId.value = null }
}
watch(() => props.visible && authStore.canAccessAllTenants, active => {
  if (active) void loadTenants()
  else { ++sequence; loading.value = false; tenants.value = []; total.value = 0; createVisible.value = false }
}, { immediate: true })
onUnmounted(() => { disposed = true; ++sequence })
</script>

<style scoped lang="less">
.manager-description,.workspace-description { color:var(--td-text-color-secondary); line-height:1.6; }
.search-controls { display:flex; gap:10px; margin:18px 0; }
.search-controls .t-input__wrap { flex:1; min-width:0; }
.create-workspace { padding:18px; margin-bottom:18px; border:1px solid var(--td-component-stroke); border-radius:var(--app-radius-md); }
.create-actions { display:flex; justify-content:flex-end; gap:8px; }
.manager-error { color:var(--td-error-color); }
.workspace-name { color:var(--td-text-color-primary); }
.workspace-description { font-size:var(--app-text-sm); overflow-wrap:anywhere; }
@media(max-width:560px) { .search-controls { flex-wrap:wrap; } .search-controls .t-input__wrap { flex-basis:100%; } }
</style>
