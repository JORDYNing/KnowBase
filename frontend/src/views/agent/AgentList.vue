<template>
  <div class="agent-list-container">
    <div class="agent-list-content">
      <div class="header" style="--wails-draggable: drag">
        <div class="header-title" style="--wails-draggable: drag">
          <div class="title-row" style="--wails-draggable: drag">
            <h2 style="--wails-draggable: drag">
              <ResourceIcon type="agent" :size="24" />
              {{ $t('agent.title') }}
            </h2>
            <div class="header-actions" style="--wails-draggable: no-drag">
              <ResourceSortControl v-model="selectedResourceSort" />
              <t-tooltip v-if="authStore.hasRole('contributor')" :content="$t('agent.createAgent')" placement="bottom">
                <t-button variant="text" theme="default" size="small" class="header-action-btn"
                  data-guide="agent-list-create" style="--wails-draggable: no-drag" @click="handleCreateAgent">
                  <template #icon>
                    <span class="btn-icon-wrapper">
                      <svg class="sparkles-icon" width="19" height="19" viewBox="0 0 20 20" fill="none"
                        xmlns="http://www.w3.org/2000/svg">
                        <path
                          d="M10 3L10.8 6.2C10.9 6.7 11.3 7.1 11.8 7.2L15 8L11.8 8.8C11.3 8.9 10.9 9.3 10.8 9.8L10 13L9.2 9.8C9.1 9.3 8.7 8.9 8.2 8.8L5 8L8.2 7.2C8.7 7.1 9.1 6.7 9.2 6.2L10 3Z"
                          fill="currentColor" stroke="currentColor" stroke-width="0.8" stroke-linecap="round"
                          stroke-linejoin="round" />
                        <path
                          d="M15.5 4L15.8 5.2C15.85 5.45 16.05 5.65 16.3 5.7L17.5 6L16.3 6.3C16.05 6.35 15.85 6.55 15.8 6.8L15.5 8L15.2 6.8C15.15 6.55 14.95 6.35 14.7 6.3L13.5 6L14.7 5.7C14.95 5.65 15.15 5.45 15.2 5.2L15.5 4Z"
                          fill="currentColor" stroke="currentColor" stroke-width="0.6" stroke-linecap="round"
                          stroke-linejoin="round" />
                        <path
                          d="M4.5 13L4.8 14.2C4.85 14.45 5.05 14.65 5.3 14.7L6.5 15L5.3 15.3C5.05 15.35 4.85 15.55 4.8 15.8L4.5 17L4.2 15.8C4.15 15.55 3.95 15.35 3.7 15.3L2.5 15L3.7 14.7C3.95 14.65 4.15 14.45 4.2 14.2L4.5 13Z"
                          fill="currentColor" stroke="currentColor" stroke-width="0.6" stroke-linecap="round"
                          stroke-linejoin="round" />
                      </svg>
                    </span>
                  </template>
                  {{ $t('agent.createAgent') }}
                </t-button>
              </t-tooltip>
            </div>
          </div>
          <p class="header-subtitle" style="--wails-draggable: drag">{{ $t('agent.subtitle') }}</p>
        </div>
      </div>
      <ResourceListToolbar :hide-scopes="authStore.isLiteMode" v-model="spaceSelection" v-model:query="keyword" :count-all="allAgentsCount"
      :count-mine="agents.length" :count-favorites="agentFavoritesCount"
      :count-recents="agentRecentsCount" />
      <div class="agent-list-main">
        <EmptyState v-if="keyword.trim() && !(loading || false) && visibleResultCount === 0" icon="search"
          :title="$t('common.noResult')">
          <t-button variant="outline" @click="keyword = ''">{{ $t('common.clear') }}</t-button>
        </EmptyState>
        <!-- creator filter removed; see KnowledgeBaseList for rationale.
             Card-level creator display + URL-state field are retained. -->

        <!-- 骨架屏占位 -->
        <div v-if="loading && agents.length === 0" class="agent-card-wrap">
          <div v-for="n in 6" :key="'skel-' + n" class="agent-card agent-card-skeleton is-skeleton">
            <div class="card-header">
              <div class="card-header-left">
                <t-skeleton animation="gradient"
                  :row-col="[[{ width: '32px', height: '32px', type: 'circle' }, { width: '40%', height: '18px' }]]" />
              </div>
            </div>
            <div class="card-content">
              <t-skeleton animation="gradient"
                :row-col="[{ width: '100%', height: '14px' }, { width: '70%', height: '14px' }]" />
            </div>
            <div class="card-bottom">
              <t-skeleton animation="gradient"
                :row-col="[[{ width: '60px', height: '22px', type: 'rect' }, { width: '60px', height: '22px', type: 'rect' }]]" />
            </div>
          </div>
        </div>

        <!-- 全部 / 收藏 / 最近：共用同一份卡片模板 -->
        <div
          v-if="(spaceSelection === 'all' || spaceSelection === 'favorites' || spaceSelection === 'recents') && filteredAgents.length > 0"
          class="agent-card-wrap">
          <template v-for="(agent, index) in filteredAgents"
            :key="agent.id">
            <!-- 内置：始终置顶。filteredAgents 在 all 视图里已经把
                 builtin 排到最前；这里只在第一张 builtin 之前打一次标题。 -->
            <div v-if="agent.isMine
              && agent.is_builtin
              && (index === 0
                || !filteredAgents[index - 1].isMine
                || !(filteredAgents[index - 1] as AgentWithUI).is_builtin)" class="agent-section-header" role="button"
              tabindex="0" :aria-expanded="!isAgentSectionCollapsed('builtin')" @click="toggleAgentSection('builtin')"
              @keydown.enter.prevent="toggleAgentSection('builtin')"
              @keydown.space.prevent="toggleAgentSection('builtin')">
              <t-icon name="app" size="14px" />
              <span>{{ $t('agent.sections.builtin') }}</span>
              <span class="agent-section-count">{{ filteredAgentSectionCounts.builtin }}</span>
              <t-icon class="agent-section-toggle"
                :name="isAgentSectionCollapsed('builtin') ? 'chevron-right' : 'chevron-down'" size="14px" />
            </div>
            <!-- 我创建的：当前 agent 是本空间 + 非内置 + 我亲手创建，且前一张
                 要么不存在、要么不是本空间、要么是内置（builtin → mine 过渡）、
                 要么是同事创建。与 KB 列表对齐。 -->
            <div v-if="agent.isMine
              && !agent.is_builtin
              && isMyAgent(agent)
              && (index === 0
                || !filteredAgents[index - 1].isMine
                || (filteredAgents[index - 1] as AgentWithUI).is_builtin
                || !isMyAgent(filteredAgents[index - 1] as AgentWithUI))" class="agent-section-header" role="button"
              tabindex="0" :aria-expanded="!isAgentSectionCollapsed('mine')" @click="toggleAgentSection('mine')"
              @keydown.enter.prevent="toggleAgentSection('mine')"
              @keydown.space.prevent="toggleAgentSection('mine')">
              <t-icon name="user" size="14px" />
              <span>{{ $t('agent.sections.mine') }}</span>
              <span class="agent-section-count">{{ filteredAgentSectionCounts.mine }}</span>
              <t-icon class="agent-section-toggle"
                :name="isAgentSectionCollapsed('mine') ? 'chevron-right' : 'chevron-down'" size="14px" />
            </div>
            <!-- 本空间 · 仅查看 / 其他成员：本空间里非内置且非我创建的同事 agent。 -->
            <div v-if="agent.isMine
              && !agent.is_builtin
              && !isMyAgent(agent)
              && (index === 0
                || !filteredAgents[index - 1].isMine
                || (filteredAgents[index - 1] as AgentWithUI).is_builtin
                || isMyAgent(filteredAgents[index - 1] as AgentWithUI))" class="agent-section-header" role="button"
              tabindex="0" :aria-expanded="!isAgentSectionCollapsed('tenantOthers')" @click="toggleAgentSection('tenantOthers')"
              @keydown.enter.prevent="toggleAgentSection('tenantOthers')"
              @keydown.space.prevent="toggleAgentSection('tenantOthers')">
              <t-icon :name="tenantSectionIconName" size="14px" />
              <span>{{ $t(tenantSectionLabelKey) }}</span>
              <span class="agent-section-count">{{ filteredAgentSectionCounts.tenantOthers }}</span>
              <t-icon class="agent-section-toggle"
                :name="isAgentSectionCollapsed('tenantOthers') ? 'chevron-right' : 'chevron-down'" size="14px" />
            </div>



          </template>
        </div>

        <!-- 我的智能体 -->





        <!-- 空状态：全部（保留创建 CTA） -->
        <EmptyState v-if="!keyword.trim() && spaceSelection === 'all' && filteredAgents.length === 0 && !loading" :title="$t('agent.empty.title')"
          :description="$t('agent.empty.description')">
          <template #icon><ResourceIcon type="agent" :size="32" /></template>
          <t-button v-if="authStore.hasRole('contributor')" theme="primary" class="agent-create-btn"
            data-guide="agent-list-create" @click="handleCreateAgent">
            <template #icon>
              <span class="btn-icon-wrapper">
                <svg class="sparkles-icon" width="18" height="18" viewBox="0 0 20 20" fill="none"
                  xmlns="http://www.w3.org/2000/svg">
                  <path
                    d="M10 3L10.8 6.2C10.9 6.7 11.3 7.1 11.8 7.2L15 8L11.8 8.8C11.3 8.9 10.9 9.3 10.8 9.8L10 13L9.2 9.8C9.1 9.3 8.7 8.9 8.2 8.8L5 8L8.2 7.2C8.7 7.1 9.1 6.7 9.2 6.2L10 3Z"
                    fill="currentColor" stroke="currentColor" stroke-width="0.8" stroke-linecap="round"
                    stroke-linejoin="round" />
                  <path
                    d="M15.5 4L15.8 5.2C15.85 5.45 16.05 5.65 16.3 5.7L17.5 6L16.3 6.3C16.05 6.35 15.85 6.55 15.8 6.8L15.5 8L15.2 6.8C15.15 6.55 14.95 6.35 14.7 6.3L13.5 6L14.7 5.7C14.95 5.65 15.15 5.45 15.2 5.2L15.5 4Z"
                    fill="currentColor" stroke="currentColor" stroke-width="0.6" stroke-linecap="round"
                    stroke-linejoin="round" />
                  <path
                    d="M4.5 13L4.8 14.2C4.85 14.45 5.05 14.65 5.3 14.7L6.5 15L5.3 15.3C5.05 15.35 4.85 15.55 4.8 15.8L4.5 17L4.2 15.8C4.15 15.55 3.95 15.35 3.7 15.3L2.5 15L3.7 14.7C3.95 14.65 4.15 14.45 4.2 14.2L4.5 13Z"
                    fill="currentColor" stroke="currentColor" stroke-width="0.6" stroke-linecap="round"
                    stroke-linejoin="round" />
                </svg>
              </span>
            </template>
            <span>{{ $t('agent.createAgent') }}</span>
          </t-button>
        </EmptyState>

        <!-- 空状态：收藏 / 最近 — 不放创建按钮，参见 KnowledgeBaseList 的同处理由 -->
        <EmptyState v-if="!keyword.trim() && spaceSelection === 'favorites' && filteredAgents.length === 0 && !loading" icon="star" :title="$t('agent.empty.favoritesTitle')"
          :description="$t('agent.empty.favoritesDescription')" />
        <EmptyState v-if="!keyword.trim() && spaceSelection === 'recents' && filteredAgents.length === 0 && !loading" icon="history" :title="$t('agent.empty.recentsTitle')"
          :description="$t('agent.empty.recentsDescription')" />
        <!-- 空状态：我的 -->

        <!-- 空状态：空间下 -->

      </div>
    </div>



    <!-- 智能体编辑器弹窗 -->
    <AgentEditorModal :visible="editorVisible" :mode="editorMode" :agent="editingAgent"
      :initialSection="editorInitialSection"
      :initialHighlightField="editorInitialHighlightField"
      :readOnly="editorMode === 'edit' && editingAgent != null && !canManageAgent(editingAgent as AgentWithUI)"
      @update:visible="editorVisible = $event" @success="handleEditorSuccess" />

    <TenantModelsGuide :when="showAgentTenantModelsGuide" variant="agent" />
    <ContextualGuide tour="agentList" :when="showAgentListContextualGuide" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin, Icon as TIcon } from 'tdesign-vue-next'
import EmptyState from '@/components/EmptyState.vue'
import ResourceIcon from '@/components/icons/ResourceIcon.vue'
import { useConfirmDelete } from '@/components/settings/useConfirmDelete'
import { deleteAgent, copyAgent, type CustomAgent } from '@/api/agent'
import { useChatResourcesStore } from '@/stores/chatResources'
import { useI18n } from 'vue-i18n'
import { createSessions } from '@/api/chat/index'
import { useSettingsStore } from '@/stores/settings'
import { useMenuStore } from '@/stores/menu'
import AgentEditorModal from './AgentEditorModal.vue'
import ContextualGuide from '@/components/ContextualGuide.vue'
import TenantModelsGuide from '@/components/TenantModelsGuide.vue'
import { focusAgentEditorSection, markContextualGuideDone } from '@/config/contextualGuides'
import { useTenantModelReadiness } from '@/composables/useTenantModelReadiness'
import { useUIStore } from '@/stores/ui'
import AgentAvatar from '@/components/AgentAvatar.vue'
import ResourceListToolbar from '@/components/ResourceListToolbar.vue'
import { matchesResourceQuery } from '@/utils/resourceListSearch'
import ResourceSortControl from '@/components/ResourceSortControl.vue'
import ResourceOriginBadge from '@/components/ResourceOriginBadge.vue'
import { shouldShowResourceOriginBadge } from '@/utils/card-list-badge'
import { useAuthStore } from '@/stores/auth'
import { useListUrlState } from '@/composables/useListUrlState'
import { useResourcePins } from '@/composables/useResourcePins'
import { integrationSectionKey } from '@/config/settingsRoute'
import {
  DEFAULT_RESOURCE_SORT,
  sortResourcesWithinGroups,
  type ResourceSortAccessors,
  type ResourceSortValue,
} from '@/utils/resourceSorting'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const uiStore = useUIStore()
const chatResources = useChatResourcesStore()
const { loaded: modelsReadyLoaded, isReadyForAgent } = useTenantModelReadiness()
const selectedResourceSort = ref<ResourceSortValue>(DEFAULT_RESOURCE_SORT)

interface AgentWithUI extends CustomAgent {
  showMore?: boolean
  /** 当前空间在对话下拉中停用（仅影响本空间） */
  disabled_by_me?: boolean
}
type DisplayAgent = AgentWithUI & { isMine: true }


const flatAgentSortAccessors: ResourceSortAccessors<any> = {
  getName: item => item?.name,
  getUpdatedAt: item => item?.updated_at,
  getCreatedAt: item => item?.created_at,
}
const displayAgentSortGroup = (item: DisplayAgent) => item.is_builtin ? 'builtin' : isMyAgent(item) ? 'mine' : 'tenantOthers'


const sortDisplayAgents = (items: readonly DisplayAgent[]) => sortResourcesWithinGroups(
  items,
  selectedResourceSort.value,
  displayAgentSortGroup,
  ['builtin', 'mine', 'tenantOthers'],
  flatAgentSortAccessors,
)
const defaultScope = 'all' as const

const { scope: spaceSelection, creator: creatorFilter, query: keyword } = useListUrlState({
  defaultScope,
  defaultCreator: 'all',
})

// Per-user favorites + recents (localStorage-backed). See useResourcePins.
const pins = useResourcePins()
const agentFavoritesCount = computed(
  () => pins.favorites.value.filter((e) => e.type === 'agent').length
)
const agentRecentsCount = computed(
  () => pins.recents.value.filter((e) => e.type === 'agent').length
)
const agents = ref<AgentWithUI[]>([])
const allAgentsCount = computed(() => agents.value.length)

const agentResourceIndex = computed(() => new Map(agents.value.map(agent => [agent.id, agent])))

const favoritesAgentList = computed<DisplayAgent[]>(() => pins.favorites.value.filter(e => e.type === 'agent').flatMap(e => { const agent = agentResourceIndex.value.get(e.id); return agent ? [{ ...agent, isMine: true as const, showMore: false }] : [] }))

const recentsAgentList = computed<DisplayAgent[]>(() => pins.recents.value.filter(e => e.type === 'agent').flatMap(e => { const agent = agentResourceIndex.value.get(e.id); return agent ? [{ ...agent, isMine: true as const, showMore: false }] : [] }))

const unsearchedFilteredAgents = computed<DisplayAgent[]>(() => sortDisplayAgents(spaceSelection.value === 'favorites' ? favoritesAgentList.value : spaceSelection.value === 'recents' ? recentsAgentList.value : agents.value.map(agent => ({ ...agent, isMine: true as const }))))

const filteredAgents = computed(() => unsearchedFilteredAgents.value.filter(item => matchesResourceQuery(item, keyword.value)))

// 「工作空间」固定保持“内置 → 我创建 → 同事创建”，用户选择只影响组内顺序。
const unsearchedSortedMineAgents = computed(() => {
  return sortResourcesWithinGroups(
    agents.value,
    selectedResourceSort.value,
    item => item.is_builtin ? 'builtin' : isMyAgent(item) ? 'mine' : 'tenantOthers',
    ['builtin', 'mine', 'tenantOthers'],
    flatAgentSortAccessors,
  )
})
const sortedMineAgents = computed(() => unsearchedSortedMineAgents.value.filter(item => matchesResourceQuery(item, keyword.value)))
const loading = ref(false)
const confirmDelete = useConfirmDelete()
const editorVisible = ref(false)
const editorMode = ref<'create' | 'edit'>('create')
const editingAgent = ref<CustomAgent | null>(null)
const editorInitialSection = ref<string>('basic')
const editorInitialHighlightField = ref<string>('')
/** 当前打开三点菜单的卡片 agent.id（用于受控弹出层，避免 computed 项无持久引用导致菜单不响应） */
const openMoreAgentId = ref<string | null>(null)

const showAgentListEmpty = computed(() => {
  if (loading.value || keyword.value.trim()) return false
  if (!authStore.hasRole('contributor')) return false
  if (spaceSelection.value === 'all' && filteredAgents.value.length === 0) return true
  return false
})

const showAgentTenantModelsGuide = computed(
  () => modelsReadyLoaded.value && showAgentListEmpty.value && !isReadyForAgent.value,
)

const showAgentListContextualGuide = computed(
  () => showAgentListEmpty.value && isReadyForAgent.value && !editorVisible.value,
)

const applyAgentListData = (res: { data: CustomAgent[]; disabled_own_agent_ids: string[] }) => {
  const disabledOwnIds = res.disabled_own_agent_ids || []
  agents.value = (res.data || []).map((agent: CustomAgent) => ({
    ...agent,
    showMore: false,
    disabled_by_me: disabledOwnIds.includes(agent.id)
  }))
  void checkAndOpenEditModal()
}
const fetchList = (force = false) => { loading.value = true; return chatResources.fetchAgentsForList({ creator: creatorFilter.value }, force).then(applyAgentListData).finally(() => { loading.value = false }) }

const resolveAgentForEdit = (id: string): CustomAgent | null => agents.value.find(agent => agent.id === id) || null


let editOpenGeneration = 0

const checkAndOpenEditModal = async () => {
  const generation = ++editOpenGeneration
  const editId = route.query.edit as string
  const section = route.query.section as string
  if (editId && (section === 'im' || section === 'embed' || section === 'integrations' || section === 'integration-im' || section === 'integration-embed')) {
    const tab = section === 'embed' || section === 'integration-embed' ? 'embed' : 'im'
    router.replace({
      path: '/platform/settings',
      query: { section: integrationSectionKey(tab), agentId: editId },
    })
    return
  }
  if (editId) {
    const agent = resolveAgentForEdit(editId)
    // A route change can remove a creator filter before the corresponding
    // all-agent fetch completes. Keep the deep-link query intact so the list
    // refresh callback can resolve and open the target instead of losing it.
    if (!agent) return

    const requestedSection = section || 'basic'
    const requestedHighlight = (route.query.highlight as string) || ''
    if (
      editorVisible.value
      && editingAgent.value?.id === agent.id
      && !requestedHighlight
    ) {
      // Global Settings may be covering this exact editor. Preserve any
      // unsaved draft and focus the requested configuration section in place.
      editorInitialSection.value = requestedSection
      focusAgentEditorSection(requestedSection)
    } else {
      // A global Settings dialog can be opened on top of an existing agent
      // editor. Flush visible=false before loading the deep-linked target so
      // AgentEditorModal's visibility watcher rebuilds its form data.
      editorVisible.value = false
      await nextTick()
      if (generation !== editOpenGeneration) return
      editingAgent.value = agent
      editorMode.value = 'edit'
      editorInitialSection.value = requestedSection
      editorInitialHighlightField.value = requestedHighlight
      editorVisible.value = true
    }
    if (generation !== editOpenGeneration) return
    // Drop the transient edit/section params but preserve other filter
    // state (scope / creator / q) so refreshing doesn't reset the view.
    const { edit: _e, section: _s, highlight: _h, ...rest } = route.query
    router.replace({ path: route.path, query: rest })
  }
}

// Also re-run when the query mutates while this view is already mounted —
// e.g. the IM overview dialog navigating here via router.push lands on the
// same route, so onMounted alone never fires and the editor would only open
// after a manual refresh.
watch(
  () => route.query.edit,
  (v) => {
    if (v && (agents.value.length > 0)) {
      void checkAndOpenEditModal()
    }
  },
)

// 监听菜单创建智能体事件
const handleOpenAgentEditor = (event: CustomEvent) => {
  if (event.detail?.mode === 'create') {
    openCreateModal()
  }
}

// Refetch when the creator filter flips so the server applies the
// predicate uniformly (also keeps built-in agents always present, see
// the matching block in custom_agent.go).
watch(creatorFilter, () => {
  fetchList(true)
})

onMounted(() => {
  fetchList()
  window.addEventListener('openAgentEditor', handleOpenAgentEditor as EventListener)
})

onUnmounted(() => {
  window.removeEventListener('openAgentEditor', handleOpenAgentEditor as EventListener)
})
const handleCardClick = (agent: DisplayAgent | AgentWithUI) => { if (openMoreAgentId.value === agent.id) return; pins.touchRecent('agent', agent.id); handleEdit(agent) }


const toggleFavoriteAgent = (agentId: string, evt?: Event) => {
  evt?.stopPropagation()
  pins.toggleFavorite('agent', agentId)
}
const isAgentFavorited = (agentId: string) => pins.isFavorite('agent', agentId)

const handleEdit = (agent: AgentWithUI) => {
  openMoreAgentId.value = null
  editingAgent.value = agent
  editorMode.value = 'edit'
  editorInitialSection.value = 'basic'
  editorInitialHighlightField.value = ''
  editorVisible.value = true
}

// canManageAgent mirrors the server-side OwnedAgentOrAdmin guard
// (PR 5 #1303): the agent's creator may always edit / delete; otherwise
// Admin+ is required. Built-in agents have created_by="" → only Admin+
// matches, which lines up with the "Admin can mutate tenant-owned
// agents" rule. The server still enforces the same matrix on every
// mutation; this gate just hides buttons the user has no authority
// to use.
function canManageAgent(agent: AgentWithUI): boolean {
  const userId = authStore.user?.id || ''
  const creatorId = (agent as any).created_by || ''
  if (creatorId && userId && creatorId === userId) return true
  return authStore.hasRole('admin')
}

// isMyAgent 仅用于卡片来源徽章在「我创建」与「同空间其他成员创建」之间切换。
// 跟 canManageAgent 区别：管理权限有 admin 兜底；徽章纯粹按 created_by 匹配。
// 内建 agent（created_by=""）也归到非 mine 一档，由模板上的 builtin 分支
// 提前拦截，不会落到 ResourceOriginBadge。
function isMyAgent(agent: { created_by?: string }): boolean {
  const userId = authStore.user?.id || ''
  return !!(agent.created_by && userId && agent.created_by === userId)
}

// agentOriginVariant 跟 kbOriginVariant 对齐：右下角徽章不再重复空间名
// （顶部 TenantSelector 已经标了空间身份），所有角色都用 creator 变体。
// 内建 agent 走 v-else 前的 builtin 分支，到不了这里。
function agentOriginVariant(agent: { created_by?: string }): 'mine' | 'creator' {
  return isMyAgent(agent) ? 'mine' : 'creator'
}

function showAgentOriginBadge(agent: { created_by?: string; creator_name?: string }): boolean {
  return shouldShowResourceOriginBadge({
    section: agentSectionOf(agent),
    variant: agentOriginVariant(agent),
    creatorName: (agent as any).creator_name,
    showSectionHeaders: true,
  })
}

function showAgentBuiltinBadge(agent: { is_builtin?: boolean }): boolean {
  if (!agent.is_builtin) return false
  return shouldShowResourceOriginBadge({
    section: agentSectionOf(agent),
    variant: 'mine',
    showSectionHeaders: true,
  })
}
// 同空间、非当前用户创建的 Agent 分组标题。
// contributor / viewer 在本空间里对这些 Agent 没有写权限，所以打"仅查看"；
// admin / owner 对整个空间都有编辑权限，"仅查看"反而误导，统一改成
// "本空间 · 其他成员"——按所有权而非权限来标注。
const tenantSectionLabelKey = computed(() =>
  authStore.hasRole('admin')
    ? 'agent.sections.tenantOthers'
    : 'agent.sections.tenantReadonly'
)

// 与 KB 列表 .tenantSectionIconName 同理：admin/owner 看到"其他成员"配
// usergroup（多人）；contributor/viewer 看到"仅查看"配 browse（眼睛）。
const tenantSectionIconName = computed(() =>
  authStore.hasRole('admin') ? 'usergroup' : 'browse'
)
type AgentSectionKey = 'builtin' | 'mine' | 'tenantOthers'

const collapsedAgentSections = ref<Set<AgentSectionKey>>(new Set())
const isAgentSectionCollapsed = (key: AgentSectionKey) => collapsedAgentSections.value.has(key)
const toggleAgentSection = (key: AgentSectionKey) => {
  const next = new Set(collapsedAgentSections.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  collapsedAgentSections.value = next
}
const agentSectionOf = (agent: { is_builtin?: boolean; created_by?: string }): AgentSectionKey => agent.is_builtin ? 'builtin' : isMyAgent(agent) ? 'mine' : 'tenantOthers'

const isAgentRowHidden = (item: any): boolean => {
  const key = agentSectionOf(item)
  return key !== null && isAgentSectionCollapsed(key)
}
const emptyAgentCounts = (): Record<AgentSectionKey, number> => ({ builtin: 0, mine: 0, tenantOthers: 0 })

const filteredAgentSectionCounts = computed<Record<AgentSectionKey, number>>(() => {
  const c = emptyAgentCounts()
  filteredAgents.value.forEach(a => {
    const key = agentSectionOf(a)
    if (key) c[key]++
  })
  return c
})
const mineAgentSectionCounts = computed<Record<AgentSectionKey, number>>(() => {
  const c = emptyAgentCounts()
  sortedMineAgents.value.forEach(a => {
    const key = agentSectionOf(a)
    if (key) c[key]++
  })
  return c
})

const handleDelete = (agent: AgentWithUI) => {
  openMoreAgentId.value = null
  confirmDelete({
    title: t('agent.delete.confirmTitle'),
    body: t('agent.delete.confirmMessage', { name: agent.name }),
    onConfirm: async () => {
      try {
        const res: any = await deleteAgent(agent.id)
        if (res.success) {
          MessagePlugin.success(t('agent.messages.deleted'))
          fetchList(true)
        } else {
          MessagePlugin.error(res.message || t('agent.messages.deleteFailed'))
        }
      } catch (e: any) {
        MessagePlugin.error(e?.message || t('agent.messages.deleteFailed'))
      }
    },
  })
}

const handleCopy = (agent: AgentWithUI) => {
  openMoreAgentId.value = null
  copyAgent(agent.id).then((res: any) => {
    if (res.data) {
      MessagePlugin.success(t('agent.messages.copied'))
      fetchList(true)
    } else {
      MessagePlugin.error(res.message || t('agent.messages.copyFailed'))
    }
  }).catch((e: any) => {
    MessagePlugin.error(e?.message || t('agent.messages.copyFailed'))
  })
}

const handleEditorSuccess = (agent?: CustomAgent) => {
  if (agent) {
    editingAgent.value = agent
    editorMode.value = 'edit'
  }
  fetchList(true)
}

// 暴露创建方法供外部调用
const openCreateModal = () => {
  editingAgent.value = null
  editorMode.value = 'create'
  editorInitialSection.value = 'basic'
  editorInitialHighlightField.value = ''
  editorVisible.value = true
}

// 创建智能体
const handleCreateAgent = () => {
  if (!isReadyForAgent.value) {
    MessagePlugin.warning(t('contextualGuide.tenantModels.needChatModelFirst'))
    uiStore.openSettings('models')
    return
  }
  markContextualGuideDone('agentList')
  openCreateModal()
}

defineExpose({
  openCreateModal
})
const visibleResultCount = computed(() => filteredAgents.value.length)

// A new search reveals matching rows even if their group was previously collapsed.
watch(keyword, () => { collapsedAgentSections.value = new Set() })
watch(spaceSelection, value => { if (!['all', 'favorites', 'recents'].includes(value)) spaceSelection.value = 'all' }, { immediate: true })
</script>

<style scoped lang="less">
@import (reference) '@/components/css/resource-card.less';

.agent-list-container {
  flex: 1;
  min-width: 0;
  min-height: 0;
  height: 100%;
  display: flex;
}

.agent-list-content { .resource-list-content(); }

.agent-list-main { .resource-list-main(); }

.agent-list-main-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  padding: 12px;
  background: var(--td-bg-color-container);
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
  padding-right: 28px;

  .header-title {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .title-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    color: var(--td-text-color-primary);
    font-family: var(--app-font-family);
    font-size: var(--app-text-4xl);
    font-weight: 600;
    line-height: 32px;
  }
}

.header-subtitle {
  margin: 0;
  color: var(--td-text-color-placeholder);
  font-family: var(--app-font-family);
  font-size: var(--app-text-base);
  font-weight: 400;
  line-height: 20px;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.header-action-btn {
  padding: 0;
  min-width: 28px;
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--td-bg-color-secondarycontainer);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-sm);
  color: var(--td-text-color-secondary);
  cursor: pointer;
  box-shadow: inset 0 1px 0 color-mix(in srgb, var(--td-bg-color-container) 72%, transparent);
  transition: background var(--app-motion-base), border-color var(--app-motion-base), color var(--app-motion-base);

  &:hover {
    background: var(--td-bg-color-secondarycontainer);
    border-color: var(--td-component-stroke);
    color: var(--td-text-color-primary);
  }

  :deep(.t-button__icon) {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    line-height: 1;
  }

  :deep(.t-icon),
  :deep(.btn-icon-wrapper) {
    color: var(--td-brand-color);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    line-height: 1;
  }
}

.card-bottom-source {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: var(--app-radius-lg);
  background: var(--td-bg-color-container-hover);
  flex-shrink: 0;
}

.card-bottom-source .org-icon {
  width: 12px;
  height: 12px;
  flex-shrink: 0;
}

.org-source-text {
  color: var(--td-text-color-secondary);
  font-family: var(--app-font-family);
  font-size: var(--app-text-xs);
  font-weight: 500;
  flex-shrink: 0;
}

.agent-section-header {
  .resource-section-header();
}

.agent-card-wrap {
  .resource-card-grid();
}

.agent-card {
  .resource-card();

  .agent-favorite-star { .resource-favorite-button(); }
}

.builtin-badge {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 8px;
  border-radius: var(--app-radius-lg);
  background: var(--td-bg-color-container-hover);
  color: var(--td-text-color-secondary);
  font-family: var(--app-font-family);
  font-size: var(--app-text-xs);
  font-weight: 500;
  flex-shrink: 0;
}

.builtin-avatar {
  border-radius: var(--app-radius-md);

  &.agent-emoji {
    font-size: var(--app-text-2xl);
    line-height: 1;
    background: var(--td-bg-color-container-hover);
  }

  &.normal {
    background: linear-gradient(135deg, color-mix(in srgb, var(--td-brand-color) 15%, transparent) 0%, color-mix(in srgb, var(--td-brand-color) 8%, transparent) 100%);
    color: var(--td-brand-color-active);
  }

  &.agent {
    background: linear-gradient(135deg, color-mix(in srgb, var(--app-accent-purple) 15%, transparent) 0%, color-mix(in srgb, var(--app-accent-purple) 8%, transparent) 100%);
    color: var(--td-brand-color);
  }
}

/* 与知识库卡片内容区一致 */
/* 三个列表卡片统一：描述字体 */
.bottom-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.feature-badge {
  .resource-feature-badge();

  &.mode-normal {
    background: color-mix(in srgb, var(--td-brand-color) 8%, transparent);
    color: var(--td-brand-color-active);

    &:hover {
      background: color-mix(in srgb, var(--td-brand-color) 12%, transparent);
    }
  }

  &.mode-agent {
    background: color-mix(in srgb, var(--app-accent-purple) 8%, transparent);
    color: var(--td-brand-color);

    &:hover {
      background: color-mix(in srgb, var(--app-accent-purple) 12%, transparent);
    }
  }

  &.web-search {
    background: color-mix(in srgb, var(--td-warning-color) 8%, transparent);
    color: var(--td-warning-color);

    &:hover {
      background: color-mix(in srgb, var(--td-warning-color) 12%, transparent);
    }
  }

  &.knowledge {
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-secondary);

    &:hover {
      background: var(--td-bg-color-container-hover);
    }
  }

  &.mcp {
    background: color-mix(in srgb, var(--td-error-color) 8%, transparent);
    color: var(--td-error-color);

    &:hover {
      background: color-mix(in srgb, var(--td-error-color) 12%, transparent);
    }
  }

  &.multi-turn {
    background: color-mix(in srgb, var(--td-brand-color) 8%, transparent);
    color: var(--td-brand-color);

    &:hover {
      background: color-mix(in srgb, var(--td-brand-color) 12%, transparent);
    }
  }
}

// 删除确认对话框样式
:deep(.t-dialog__position.t-dialog--top) {
  padding-top: 40vh !important;
}

.resource-list-header();

</style>

<style lang="less">
/* 下拉菜单样式已统一至 @/assets/dropdown-menu.less */


</style>
