import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'

const require = createRequire(import.meta.url)
const renderer = vue.createRenderer<any, any>({
  createElement: tag => ({ tag, children: [], text: '' }), createText: text => ({ text }), createComment: () => ({}),
  insert(child, parent, anchor) {
    if (child.parent) { const siblings = child.parent.children; const index = siblings.indexOf(child); if (index >= 0) siblings.splice(index, 1) }
    const children = parent.children ?? (parent.children = [])
    const index = anchor ? children.indexOf(anchor) : -1
    children.splice(index < 0 ? children.length : index, 0, child); child.parent = parent
  },
  remove(child) { const children = child.parent?.children; const index = children?.indexOf(child); if (index >= 0) children.splice(index, 1) },
  setElementText(el, text) { el.text = text }, setText(el, text) { el.text = text }, patchProp() {},
  parentNode: node => node.parent, nextSibling: node => { const children = node.parent?.children ?? []; return children[children.indexOf(node) + 1] },
})
async function settle() { await new Promise<void>(resolve => setImmediate(resolve)); await vue.nextTick() }
function mount(file: string, auth: any, api: any, rendered = false) {
  const { descriptor } = parse(readFileSync(new URL(file, import.meta.url), 'utf8'))
  const script = compileScript(descriptor, { id: 'platform-manager-test' })
  let source = script.content.replace('__expose();', '').replace('return __returned__', '__expose(__returned__); return __returned__')
  if (rendered) source += '\n' + compileTemplate({ id: 'platform-manager-test', source: descriptor.template!.content, filename: file, compilerOptions: { bindingMetadata: script.bindings } }).code
  const exports: any = {}
  const destinations: string[] = []
  const modules: Record<string, any> = {
    vue: { ...vue, Transition: vue.defineComponent({ inheritAttrs: false, setup(_, { slots }) { return () => slots.default?.() } }) }, 'vue-router': { useRouter: () => ({ push() {} }) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: { success() {} } },
    '@/api/tenant': api, '@/api/auth': { logout() {} },
    '@/stores/auth': { useAuthStore: () => auth },
    '@/stores/ui': { useUIStore: () => ({ sidebarCollapsed: false }) },
    '@/composables/useRoleLabel': { useRoleLabel: () => ({ formatRole: () => '', roleIcon: () => '' }) },
    '@/config/contextualGuides': { openNewUserGuide() {} },
    '@/config/settingsAccess': { SETTINGS_MANAGEMENT_SHORTCUT_MIN_ROLE: { models: 'admin' } },
    '@/components/PlatformWorkspaceManager.vue': { __esModule: true, default: { render: () => null } },
  }
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  runInNewContext(code, { exports, console, Number, window: { location: { assign: (path: string) => destinations.push(path) } }, document: { addEventListener() {}, removeEventListener() {} }, require(name: string) { return modules[name] ?? require(name) } })
  const component = exports.default
  component.render = rendered ? exports.render : () => null
  const instance = vue.ref<any>()
  const tree = { children: [] }
  const app = renderer.createApp({ render: () => vue.h(component, { visible: true, ref: instance }) })
  app.config.globalProperties.$t = (key: string) => key
  const stub = vue.defineComponent({ setup(_, { slots }) { return () => vue.h('div', slots.default?.()) } })
  for (const name of ['t-icon', 't-tooltip']) app.component(name, stub)
  app.mount(tree)
  return { vm: instance.value, tree, destinations, unmount: () => app.unmount() }
}
function fixture(canAll = true, systemAdmin = false) {
  const calls: any[] = []
  const auth = vue.reactive({ canAccessAllTenants: canAll, isSystemAdmin: systemAdmin, tenant: { id: '10000' }, user: { username: 'User' }, hasValidTenant: true, isLiteMode: false, hasRole: () => false, ensureAuthMe: async () => true,
    async switchPlatformWorkspace(id: number) { calls.push(['switch', id]); return true },
  })
  const api = {
    async searchTenants(params: any) { calls.push(['search', params]); return { success: true, data: { items: [{ id: 10001, name: 'Operations', status: 'active' }], total: 1 } } },
    async createTenant(payload: any) { calls.push(['create', payload]); return { success: true, data: { id: 10002, ...payload } } },
  }
  return { auth, api, calls }
}
function textTree(node: any): string { return [node.text ?? '', ...(node.children ?? []).map(textTree)].join(' ') }

test('ordinary members and system admins have no management entry or background catalog calls', async () => {
  for (const systemAdmin of [false, true]) {
    const f = fixture(false, systemAdmin)
    const menu = mount('./UserMenu.vue', f.auth, f.api, true)
    menu.vm.menuVisible = true; await settle()
    assert.equal(textTree(menu.tree).includes('platformWorkspaces.title'), false)
    menu.vm.openPlatformWorkspaces(); await settle()
    assert.equal(menu.vm.platformWorkspacesVisible, false)
    menu.unmount()
    const manager = mount('./PlatformWorkspaceManager.vue', f.auth, f.api)
    await settle()
    manager.vm.name = 'Should not create'
    await manager.vm.loadTenants(); await manager.vm.submitCreate(); await manager.vm.switchWorkspace(10001)
    assert.equal(f.calls.length, 0)
    manager.unmount()
  }
})

test('catalog superusers can find, create, and switch workspaces; revoked capability stops requests', async () => {
  const f = fixture()
  const menu = mount('./UserMenu.vue', f.auth, f.api, true)
  menu.vm.menuVisible = true; await settle()
  assert.equal(textTree(menu.tree).includes('platformWorkspaces.title'), true, textTree(menu.tree))
  menu.vm.openPlatformWorkspaces(); assert.equal(menu.vm.platformWorkspacesVisible, true)
  menu.unmount()
  const manager = mount('./PlatformWorkspaceManager.vue', f.auth, f.api)
  await settle()
  assert.equal(f.calls.length, 1)
  manager.vm.keyword = ' Operations '; manager.vm.search(); await settle()
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls[1])), ['search', { keyword: 'Operations', page: 1, page_size: 10 }])
  manager.vm.openCreate(); manager.vm.name = ' Research '; manager.vm.description = ' Team research '
  await manager.vm.submitCreate()
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls[2])), ['create', { name: 'Research', description: 'Team research' }])
  assert.equal(manager.vm.createVisible, false)
  await manager.vm.switchWorkspace(10001)
  assert.deepEqual(f.calls.at(-1), ['switch', 10001])
  assert.deepEqual(manager.destinations, ['/platform/knowledge-bases'])
  f.auth.canAccessAllTenants = false; await settle()
  const before = f.calls.length
  manager.vm.name = 'Blocked'
  await manager.vm.loadTenants(); await manager.vm.submitCreate(); await manager.vm.switchWorkspace(10002)
  assert.equal(f.calls.length, before)
  assert.equal(manager.vm.tenants.length, 0)
  manager.unmount()
})
