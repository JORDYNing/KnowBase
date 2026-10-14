import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as stateHelpers from './teamAccessState.ts'
import type { MyTenantJoinRequest, TenantJoinRequest } from '@/api/tenant/join-requests'

const require = createRequire(import.meta.url)
const renderer = vue.createRenderer<any, any>({
  createElement: () => ({}), createText: () => ({}), createComment: () => ({}),
  insert() {}, remove() {}, setElementText() {}, setText() {}, patchProp() {},
  parentNode: () => null, nextSibling: () => null,
})
const request: TenantJoinRequest = { id: 9, tenant_id: 10000, user_id: 'u', source: 'feishu', status: 'pending', version: 3, created_at: '', updated_at: '', username: 'User', email: 'u@example.com' }
const pending: MyTenantJoinRequest = { request, tenant: { id: 10000, name: 'Team' }, membership: null, target_tenant_id: 10000, target_available: true, can_resubmit: false, access_revoked: false }
async function settle() { await new Promise<void>(resolve => setImmediate(resolve)); await vue.nextTick() }

function mount(path: string, modules: Record<string, any>, props: Record<string, any> = {}, globals: Record<string, any> = {}) {
  const { descriptor } = parse(readFileSync(new URL(path, import.meta.url), 'utf8'))
  const script = compileScript(descriptor, { id: 'team-flow-test' }).content
    .replace('__expose();', '')
    .replace('return __returned__', '__expose(__returned__); return __returned__')
  const exports: any = {}
  const compiled = ts.transpileModule(script, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const common: Record<string, any> = {
    vue,
    '@/components/PlatformWorkspaceManager.vue': { __esModule: true, default: { render: () => null } },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key, locale: vue.ref('en-US') }) },
    'tdesign-vue-next': { MessagePlugin: { success() {} } },
    './teamAccessState': stateHelpers,
    '@/views/auth/teamAccessState': stateHelpers,
  }
  runInNewContext(compiled, { exports, console, ...globals, require(name: string) {
    return modules[name] ?? common[name] ?? require(name)
  } })
  const component = exports.default
  component.render = () => null
  const instance = vue.ref<any>()
  const app = renderer.createApp({ render: () => vue.h(component, { ...props, ref: instance }) })
  app.mount({})
  return { vm: instance.value, unmount: () => app.unmount() }
}
function onboardingFixture(initial: MyTenantJoinRequest = pending) {
  let snapshot = initial
  let calls = 0
  let entries = 0
  const submits: unknown[] = []
  const destinations: string[] = []
  const intervals = new Map<number, { fn: () => void; ms: number }>()
  const listeners = new Map<string, () => void>()
  const document = { visibilityState: 'visible', addEventListener: (name: string, fn: () => void) => listeners.set(name, fn), removeEventListener: (name: string) => listeners.delete(name) }
  const auth = { user: { email: 'u@example.com' }, hasValidTenant: false, async enterTeamWorkspace() { entries++; auth.hasValidTenant = true; return true }, logout() {} }
  const mounted = mount('./WorkspaceOnboarding.vue', {
    'vue-router': { useRouter: () => ({ replace: async (path: string) => destinations.push(path) }) },
    '@/stores/auth': { useAuthStore: () => auth },
    '@/api/auth': { logout: async () => ({ success: true }) },
    '@/api/tenant/join-requests': {
      getMyTenantJoinRequest: async () => { calls++; return { success: true, data: snapshot } },
      submitTenantJoinRequest: async (version: unknown) => { submits.push(version); snapshot = { ...pending, request: { ...request, version: 4 } }; return { success: true, data: snapshot } },
    },
  }, {}, { document, setInterval: (fn: () => void, ms: number) => { const id = intervals.size + 1; intervals.set(id, { fn, ms }); return id }, clearInterval: (id: number) => intervals.delete(id) })
  return { ...mounted, intervals, listeners, document, submits, destinations, get calls() { return calls }, get entries() { return entries }, setSnapshot(value: MyTenantJoinRequest) { snapshot = value } }
}

test('pending onboarding polls only while visible and stops on unmount', async () => {
  const f = onboardingFixture()
  await settle()
  assert.equal(f.calls, 1)
  assert.equal(f.intervals.size, 1)
  assert.equal([...f.intervals.values()][0]!.ms, 30_000)
  f.document.visibilityState = 'hidden'; f.listeners.get('visibilitychange')!()
  assert.equal(f.intervals.size, 0)
  f.document.visibilityState = 'visible'; f.listeners.get('visibilitychange')!()
  await settle()
  assert.equal(f.calls, 2)
  assert.equal(f.entries, 0)
  f.unmount()
  assert.equal(f.intervals.size, 0)
  assert.equal(f.listeners.size, 0)
})

test('rejected onboarding waits for explicit resubmission and sends the current version', async () => {
  const f = onboardingFixture({ ...pending, can_resubmit: true, request: { ...request, status: 'rejected' } })
  await settle()
  assert.equal(f.submits.length, 0)
  assert.equal(f.intervals.size, 0)
  await f.vm.submitRequest(); await settle()
  assert.deepEqual(f.submits, [3])
  assert.equal(f.vm.state, 'pending')
  assert.equal(f.intervals.size, 1)
  f.unmount()
})

test('active approval exchanges credentials before entering the team; revoked approval cannot enter', async () => {
  const ready = onboardingFixture({ ...pending, membership: { tenant_id: 10000, role: 'viewer' } })
  await settle()
  assert.equal(ready.entries, 1)
  assert.deepEqual(ready.destinations, ['/platform/knowledge-bases'])
  ready.unmount()
  const revoked = onboardingFixture({ ...pending, request: { ...request, status: 'approved' }, access_revoked: true })
  await settle()
  assert.equal(revoked.entries, 0)
  assert.equal(revoked.vm.canSubmit, false)
  assert.equal(revoked.vm.state, 'revoked')
  revoked.unmount()
})

test('approval requires an explicit allowed role and includes the optimistic version', async () => {
  const reviews: any[] = []
  const f = mount('../settings/TenantJoinRequests.vue', {
    '@/api/tenant/join-requests': {
      listTenantJoinRequests: async () => ({ success: true, data: { requests: [request], total: 1 } }),
      reviewTenantJoinRequest: async (...args: any[]) => { reviews.push(args); return { success: true, data: { ...request, status: 'approved' } } },
    },
  }, { tenantId: 10000, currentRole: 'admin' })
  await settle()
  f.vm.openReview(request, true)
  assert.equal(f.vm.selectedRole, '')
  await f.vm.reviewRequest()
  assert.equal(reviews.length, 0)
  f.vm.selectedRole = 'admin'
  await f.vm.reviewRequest()
  assert.equal(reviews.length, 0)
  f.vm.selectedRole = 'contributor'
  await f.vm.reviewRequest()
  assert.equal(reviews.length, 1)
  assert.equal(reviews[0][0], 10000)
  assert.equal(reviews[0][1], 9)
  assert.deepEqual(JSON.parse(JSON.stringify(reviews[0][2])), { approved: true, role: 'contributor', version: 3 })
  f.unmount()
})
