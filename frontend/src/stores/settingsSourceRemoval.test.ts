import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'

test('loading saved settings removes the retired source workspace while preserving selections', () => {
  const storage = new Map<string, string>([['WeKnora_settings', JSON.stringify({
    selectedAgentId: 'team-agent', selectedAgentSourceTenantId: '42', isAgentEnabled: true,
    selectedKnowledgeBases: ['kb-team'], selectedTags: [], selectedMCPServices: [], selectedSkills: [], selectedFileKbMap: {},
  })]])
  const exports: any = {}
  const source = readFileSync(new URL('./settingsStorage.ts', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  runInNewContext(compiled, {
    exports, console,
    localStorage: { getItem: (key: string) => storage.get(key) ?? null },
    require(name: string) {
      if (name === '@/composables/preferenceStorage') return {
        safeSetItem: (key: string, value: string) => storage.set(key, value), safeRemoveItem: (key: string) => storage.delete(key),
      }
      if (name === '@/utils/agent-mode') return { reconcileBuiltinAgentMode: () => false }
      throw new Error(name)
    },
  })
  const loaded = exports.loadAndReconcileSettings({ isAgentEnabled: false })
  assert.equal(Object.hasOwn(loaded, 'selectedAgentSourceTenantId'), false)
  assert.equal(loaded.selectedAgentId, 'team-agent')
  assert.deepEqual(JSON.parse(JSON.stringify(loaded.selectedKnowledgeBases)), ['kb-team'])
  assert.equal(Object.hasOwn(JSON.parse(storage.get('WeKnora_settings')!), 'selectedAgentSourceTenantId'), false)
})
