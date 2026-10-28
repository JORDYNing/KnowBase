import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { buildYouTubeTranscriptUpdate, safeYouTubeResultLink, youtubeKnowledgeBaseOptions } from './youtube-transcript'
import { evaluateToolRequirement } from './tool-capabilities'
import { canAccessToolboxSection, toolboxLocation } from '../config/toolbox'

test('proxy settings preserve stored credentials unless replacement or removal is explicit', () => {
  assert.deepEqual(buildYouTubeTranscriptUpdate(true, 'kb', '', false), { enabled: true, knowledge_base_id: 'kb' })
  assert.deepEqual(buildYouTubeTranscriptUpdate(false, 'kb', ' http://proxy:3128 ', false), {
    enabled: false, knowledge_base_id: 'kb', proxy_url: 'http://proxy:3128',
  })
  assert.deepEqual(buildYouTubeTranscriptUpdate(true, 'kb', 'http://new-proxy', true), {
    enabled: true, knowledge_base_id: 'kb', clear_proxy: true,
  })
})

test('destination options omit FAQ and foreign workspace knowledge bases', () => {
  assert.deepEqual(youtubeKnowledgeBaseOptions([
    { id: 'a', name: 'Docs', type: 'document', tenant_id: 2 },
    { id: 'b', name: 'FAQ', type: 'faq', tenant_id: 2 },
    { id: 'c', name: 'Foreign', type: 'document', tenant_id: 3 },
    { id: 'd', name: 'Old document', tenant_id: '2' },
  ], 2), [{ label: 'Docs', value: 'a' }, { label: 'Old document', value: 'd' }])
})

test('result links reject active content and protocol-relative navigation', () => {
  for (const url of ['javascript:alert(1)', 'data:text/html,hi', '//example.org', '/\\example.org', 'https://name:secret@example.org', '/\n/path']) {
    assert.equal(safeYouTubeResultLink(url, true), '')
  }
  assert.equal(safeYouTubeResultLink('/platform/knowledge/kb?id=doc', true), '/platform/knowledge/kb?id=doc')
  assert.equal(safeYouTubeResultLink('https://www.youtube.com/watch?v=video'), 'https://www.youtube.com/watch?v=video')
})

test('YouTube configuration is admin-only and does not inherit browser access', () => {
  const roles = ['viewer', 'contributor', 'admin', 'owner']
  for (const role of roles) {
    assert.equal(canAccessToolboxSection('youtube', {
      currentTenantRole: role, canAccessAllTenants: false,
      hasRole: min => roles.indexOf(role) >= roles.indexOf(min), isSupported: () => true,
    }), ['admin', 'owner'].includes(role))
  }
  assert.deepEqual(toolboxLocation('youtube'), { path: '/platform/toolbox/youtube', query: {} })
})

test('import tool remains selectable without retrieval scope and renders a structured result', () => {
  assert.deepEqual(evaluateToolRequirement('youtube_transcript_import', {
    vector: false, keyword: false, wiki: false, graph: false, faq: false,
  }, false), { ok: true, missKind: 'none' })
  const editor = readFileSync(new URL('../views/agent/AgentEditorModal.vue', import.meta.url), 'utf8')
  assert.match(editor, /value: 'youtube_transcript_import'.*group: 'import'/)
  const renderer = readFileSync(new URL('../views/chat/components/ToolResultRenderer.vue', import.meta.url), 'utf8')
  assert.match(renderer, /YouTubeTranscriptResult v-else-if="displayType === 'youtube_transcript_import'"/)
})

test('configuration page clears saved proxy input and reloads when workspace changes', async () => {
  const { runInNewContext } = await import('node:vm')
  const ts = (await import('typescript')).default
  const { ref } = await import('vue')
  const source = readFileSync(new URL('../views/settings/YouTubeTranscriptSettings.vue', import.meta.url), 'utf8')
  const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
    .replace(/^import .*$/gm, '')
  let watchSource: (() => unknown) | undefined
  let watchCallback: (() => Promise<void>) | undefined
  const auth = { currentTenantId: 1 }
  let readResult: Promise<unknown> = Promise.resolve({ data: { enabled: true, knowledge_base_id: 'one', proxy_url: 'http://***:***@proxy' } })
  const payloads: unknown[] = []
  const state = runInNewContext(ts.transpileModule(`${script}\n;({enabled, knowledgeBaseId, proxyInput, proxyConfigured, clearProxy, load, save});`, {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText, {
    ref, useI18n: () => ({ t: (key: string) => key }), useAuthStore: () => auth,
    watch: (source: () => unknown, callback: () => Promise<void>) => { watchSource = source; watchCallback = callback },
    MessagePlugin: { success() {}, error() {} },
    listKnowledgeBases: async () => ({ data: [] }),
    getYouTubeTranscriptConfig: () => readResult,
    updateYouTubeTranscriptConfig: async (payload: unknown) => {
      payloads.push(payload)
      return { data: { enabled: true, knowledge_base_id: 'one', proxy_url: 'http://***:***@proxy' } }
    },
    buildYouTubeTranscriptUpdate, youtubeKnowledgeBaseOptions,
  })
  await state.load()
  assert.equal(state.proxyConfigured.value, true)
  assert.equal(state.proxyInput.value, '')
  await state.save()
  assert.deepEqual(payloads[0], { enabled: true, knowledge_base_id: 'one' })
  let resolveOld!: (value: unknown) => void
  readResult = new Promise(resolve => { resolveOld = resolve })
  const oldLoad = state.load()
  auth.currentTenantId = 2
  assert.equal(watchSource!(), 2)
  readResult = Promise.resolve({ data: { enabled: false, knowledge_base_id: 'two' } })
  await watchCallback!()
  resolveOld({ data: { enabled: true, knowledge_base_id: 'old' } })
  await oldLoad
  assert.equal(state.knowledgeBaseId.value, 'two')
  assert.equal(state.proxyConfigured.value, false)
})
