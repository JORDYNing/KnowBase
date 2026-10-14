import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'

function fixture() {
  const calls: Array<{ method: string; url: string; body: any }> = []
  const exports: any = {}
  const request = Object.fromEntries(['get', 'post', 'put'].map(method => [method, async (url: string, body?: any) => {
    calls.push({ method, url, body })
    return { success: true }
  }]))
  const source = readFileSync(new URL('./join-requests.ts', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  runInNewContext(compiled, { exports, require(name: string) {
    assert.equal(name, '@/utils/request')
    return request
  } })
  return { api: exports, calls }
}

test('request state is tenant-free and resubmission sends the last reviewed version', async () => {
  const { api, calls } = fixture()
  await api.getMyTenantJoinRequest()
  await api.submitTenantJoinRequest()
  await api.submitTenantJoinRequest(4)
  assert.deepEqual(JSON.parse(JSON.stringify(calls)), [
    { method: 'get', url: '/api/v1/me/tenant-join-request' },
    { method: 'post', url: '/api/v1/me/tenant-join-request', body: {} },
    { method: 'post', url: '/api/v1/me/tenant-join-request', body: { version: 4 } },
  ])
})
test('review and pagination use the team endpoint and preserve role, message, and version', async () => {
  const { api, calls } = fixture()
  await api.listTenantJoinRequests(10000, { status: 'pending', page: 2, page_size: 20 })
  await api.reviewTenantJoinRequest(10000, 9, { approved: true, role: 'viewer', version: 3 })
  await api.reviewTenantJoinRequest(10000, 10, { approved: false, message: 'Please contact the team.', version: 1 })
  assert.deepEqual(JSON.parse(JSON.stringify(calls)), [
    { method: 'get', url: '/api/v1/tenants/10000/join-requests', body: { params: { status: 'pending', page: 2, page_size: 20 } } },
    { method: 'put', url: '/api/v1/tenants/10000/join-requests/9/review', body: { approved: true, role: 'viewer', version: 3 } },
    { method: 'put', url: '/api/v1/tenants/10000/join-requests/10/review', body: { approved: false, message: 'Please contact the team.', version: 1 } },
  ])
})
