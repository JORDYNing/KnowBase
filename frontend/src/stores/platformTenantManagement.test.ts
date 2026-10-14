import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import * as vue from 'vue'
import { createSequencedRefresh } from './sequencedRefresh.ts'
import { hasAccessibleWorkspace } from '../views/auth/teamAccessState.ts'

function loadModule(path: string, modules: Record<string, any>, globals: Record<string, any> = {}) {
  const exports: any = {}
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  runInNewContext(compiled, { exports, console, ...globals, require(name: string) {
    assert.ok(name in modules, `Unexpected dependency: ${name}`)
    return modules[name]
  } })
  return exports
}
function storage() {
  const values = new Map<string, string>()
  return { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key) }
}

test('platform catalog APIs reject ordinary and system-admin-only clients before HTTP', async () => {
  const auth = { canAccessAllTenants: false, isSystemAdmin: true }
  const calls: any[] = []
  const api = loadModule('../api/tenant/index.ts', {
    '@/utils/request': Object.fromEntries(['get', 'post', 'put', 'del'].map(method => [method, async (...args: any[]) => { calls.push([method, ...args]); return { success: true } }])),
    '@/i18n': { __esModule: true, default: { global: { t: (key: string) => key } } },
    '@/stores/auth': { useAuthStore: () => auth },
  }, { URLSearchParams })
  await assert.rejects(api.searchTenants(), { status: 403 })
  await assert.rejects(api.listAllTenants(), { status: 403 })
  await assert.rejects(api.createTenant({ name: 'Blocked' }), { status: 403 })
  assert.equal(calls.length, 0)
  auth.canAccessAllTenants = true
  await api.searchTenants({ keyword: 'Research & design', page: 2, page_size: 10 })
  await api.createTenant({ name: 'Research', description: 'Team' })
  assert.deepEqual(JSON.parse(JSON.stringify(calls)), [
    ['get', '/api/v1/tenants/search?keyword=Research+%26+design&page=2&page_size=10'],
    ['post', '/api/v1/tenants', { name: 'Research', description: 'Team' }],
  ])
})

test('platform switching exchanges the latest refresh token, refreshes identity, and clears prior tenant resources', async () => {
  const localStorage = storage()
  const calls: any[] = []
  let target = 10000
  let deferNextMe = false
  let resolvePreviousMe: (() => void) | undefined
  let cacheClears = 0
  const user = { id: 'u', email: 'u@example.com', username: 'User', can_access_all_tenants: false, is_system_admin: true }
  const authApi = {
    userInfoFromApi: (u: any) => ({ ...u }),
    async switchTenant(id: number, refresh: string) { calls.push(['switch', id, refresh]); target = id; return { success: true, token: 'new-access', refresh_token: 'new-refresh' } },
    async getCurrentUser() {
      calls.push(['me', localStorage.getItem('weknora_token')])
      const response = { success: true, data: { user: { ...user, can_access_all_tenants: true }, tenant: { id: target, name: 'Research', status: 'active' }, memberships: [] } }
      if (deferNextMe) {
        deferNextMe = false
        return await new Promise(resolve => { resolvePreviousMe = () => resolve(response) })
      }
      return response
    },
  }
  const noOp = () => {}
  const { useAuthStore } = loadModule('./auth.ts', {
    pinia: { defineStore: (_: string, setup: () => any) => () => vue.reactive(setup()) }, vue,
    '@/api/auth': authApi, '@/i18n': { __esModule: true, default: { global: { t: (key: string) => key } } },
    '@/composables/useFont': { reloadFontFromStorage: noOp }, '@/composables/useTheme': { reloadThemeFromStorage: noOp },
    '@/composables/preferenceStorage': { resetMigrationLatch: noOp }, '@/api/agent': { BUILTIN_QUICK_ANSWER_ID: 'quick-answer' },
    '@/stores/chatResources': { useChatResourcesStore: () => ({ invalidate() { cacheClears++ } }) },
    '@/stores/editorResources': { useEditorResourcesStore: () => ({ invalidate() { cacheClears++ } }) },
    '@/stores/sequencedRefresh': { createSequencedRefresh }, '@/views/auth/teamAccessState': { hasAccessibleWorkspace },
  }, { localStorage, sessionStorage: storage() })
  const auth = useAuthStore()
  auth.setUser(user)
  assert.equal(await auth.switchPlatformWorkspace(10001), false)
  assert.equal(calls.length, 0)
  auth.setUser({ ...user, can_access_all_tenants: true })
  auth.setTenant({ id: '10000', name: 'Team', status: 'active' })
  auth.setKnowledgeBases([{ id: 'old-kb' }]); auth.setCurrentKnowledgeBase({ id: 'old-kb' })
  auth.setRefreshToken('stale-memory-refresh')
  localStorage.setItem('weknora_refresh_token', 'rotated-refresh')
  localStorage.setItem('WeKnora_settings', JSON.stringify({ selectedAgentId: 'old-agent', selectedKnowledgeBases: ['old-kb'] }))
  auth.setToken('old-access')
  deferNextMe = true
  const previousRefresh = auth.refreshFromAuthMe()
  await new Promise<void>(resolve => setImmediate(resolve))
  const tenantWrites: unknown[] = []
  const stopWatching = vue.watch(() => auth.tenant?.id, value => tenantWrites.push(value), { flush: 'sync' })
  const switching = auth.switchPlatformWorkspace(10001)
  await new Promise<void>(resolve => setImmediate(resolve))
  assert.equal(auth.tenant, null)
  assert.equal(localStorage.getItem('weknora_token'), 'new-access')
  resolvePreviousMe!()
  assert.equal(await previousRefresh, false, 'the old identity response must be discarded')
  assert.equal(await switching, true)
  stopWatching()
  assert.equal(tenantWrites.includes('10000'), false, 'old workspace must never be written back after switching')
  assert.deepEqual(calls, [['me', 'old-access'], ['switch', 10001, 'rotated-refresh'], ['me', 'new-access']])
  assert.equal(localStorage.getItem('weknora_refresh_token'), 'new-refresh')
  assert.equal(auth.hasValidTenant, true)
  assert.equal(auth.effectiveTenantId, 10001)
  assert.equal(auth.currentTenantRole, 'admin')
  assert.equal(auth.knowledgeBases.length, 0)
  assert.equal(auth.currentKnowledgeBase, null)
  assert.equal(cacheClears, 2)
  assert.deepEqual(JSON.parse(localStorage.getItem('WeKnora_settings')!).selectedKnowledgeBases, [])
  assert.equal(localStorage.getItem('weknora_selected_tenant_id'), null)
})

test('credential exchange keeps team entry available but guards other tenant IDs before HTTP', async () => {
  const auth = { canAccessAllTenants: false, isSystemAdmin: true }
  const calls: any[] = []
  const api = loadModule('../api/auth/index.ts', {
    '@/utils/request': { post: async (...args: any[]) => { calls.push(args); return { success: true } } },
    '@/i18n': { __esModule: true, default: { global: { t: (key: string) => key } } },
    '@/stores/auth': { useAuthStore: () => auth },
  })
  await assert.rejects(api.switchTenant(10001, 'refresh'), { status: 403 })
  assert.equal(calls.length, 0)
  await api.switchTenant(10000, 'team-refresh')
  auth.canAccessAllTenants = true
  await api.switchTenant(10001, 'platform-refresh')
  assert.deepEqual(JSON.parse(JSON.stringify(calls)), [
    ['/api/v1/auth/switch-tenant', { tenant_id: 10000, refresh_token: 'team-refresh' }],
    ['/api/v1/auth/switch-tenant', { tenant_id: 10001, refresh_token: 'platform-refresh' }],
  ])
})
