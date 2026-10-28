<template>
  <div class="youtube-settings" :aria-busy="loading || saving">
    <t-loading :loading="loading">
      <t-alert v-if="loadFailed" theme="error" :message="t('youtubeTranscript.loadFailed')">
        <template #operation><t-button variant="text" @click="load">{{ t('youtubeTranscript.retry') }}</t-button></template>
      </t-alert>
      <form v-else class="youtube-form" @submit.prevent="save">
        <div class="setting-row">
          <div class="setting-info">
            <label for="youtube-enabled">{{ t('youtubeTranscript.enable') }}</label>
            <p>{{ t('youtubeTranscript.enableDescription') }}</p>
          </div>
          <t-switch id="youtube-enabled" v-model="enabled" :disabled="loading || saving" :aria-label="t('youtubeTranscript.enable')" />
        </div>
        <div class="setting-row">
          <div class="setting-info">
            <label for="youtube-kb">{{ t('youtubeTranscript.knowledgeBase') }}</label>
            <p>{{ t('youtubeTranscript.knowledgeBaseDescription') }}</p>
          </div>
          <t-select id="youtube-kb" v-model="knowledgeBaseId" :options="kbOptions" filterable clearable
            :disabled="loading || saving" :placeholder="t('youtubeTranscript.chooseKnowledgeBase')"
            :aria-label="t('youtubeTranscript.knowledgeBase')" class="setting-control" />
        </div>
        <div class="setting-row">
          <div class="setting-info">
            <label for="youtube-proxy">{{ t('youtubeTranscript.proxy') }}</label>
            <p>{{ t('youtubeTranscript.proxyDescription') }}</p>
            <p v-if="proxyConfigured">{{ t('youtubeTranscript.proxyConfigured') }}</p>
          </div>
          <div class="setting-control">
            <t-input id="youtube-proxy" v-model="proxyInput" type="password" autocomplete="new-password"
              :disabled="loading || saving || clearProxy" :placeholder="'http://proxy:port'"
              :aria-label="t('youtubeTranscript.proxy')" />
            <t-checkbox v-if="proxyConfigured" v-model="clearProxy" :disabled="loading || saving" class="clear-proxy">
              {{ t('youtubeTranscript.clearProxy') }}
            </t-checkbox>
          </div>
        </div>
        <t-alert theme="info" :title="t('youtubeTranscript.languageTitle')" :message="t('youtubeTranscript.languageDescription')" />
        <p class="usage-note">{{ t('youtubeTranscript.feishuDescription') }}</p>
        <div class="settings-actions">
          <t-button type="submit" :loading="saving" :disabled="loading || (enabled && !knowledgeBaseId)">{{ t('youtubeTranscript.save') }}</t-button>
        </div>
      </form>
    </t-loading>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { getYouTubeTranscriptConfig, updateYouTubeTranscriptConfig, type YouTubeTranscriptConfig } from '@/api/youtube-transcript'
import { buildYouTubeTranscriptUpdate, youtubeKnowledgeBaseOptions } from '@/utils/youtube-transcript'

const { t } = useI18n()
const auth = useAuthStore()
const enabled = ref(false)
const knowledgeBaseId = ref('')
const proxyInput = ref('')
const proxyConfigured = ref(false)
const clearProxy = ref(false)
const kbOptions = ref<{ label: string; value: string }[]>([])
const loading = ref(true)
const saving = ref(false)
const loadFailed = ref(false)
let generation = 0

function applyConfig(config: YouTubeTranscriptConfig | null | undefined) {
  enabled.value = config?.enabled === true
  knowledgeBaseId.value = config?.knowledge_base_id || ''
  proxyConfigured.value = Boolean(config?.proxy_configured || config?.proxy_url)
  proxyInput.value = ''
  clearProxy.value = false
}

async function load() {
  const current = ++generation
  loading.value = true
  loadFailed.value = false
  applyConfig(null)
  kbOptions.value = []
  try {
    const [config, bases] = await Promise.all([getYouTubeTranscriptConfig(), listKnowledgeBases()])
    if (current !== generation) return
    applyConfig(config.data)
    kbOptions.value = youtubeKnowledgeBaseOptions(Array.isArray(bases.data) ? bases.data : [], auth.currentTenantId)
  } catch {
    if (current === generation) loadFailed.value = true
  } finally {
    if (current === generation) loading.value = false
  }
}

async function save() {
  if (loading.value || saving.value || loadFailed.value || (enabled.value && !knowledgeBaseId.value)) return
  const current = generation
  saving.value = true
  try {
    const response = await updateYouTubeTranscriptConfig(buildYouTubeTranscriptUpdate(
      enabled.value, knowledgeBaseId.value, proxyInput.value, clearProxy.value,
    ))
    if (current !== generation) return
    applyConfig(response.data)
    MessagePlugin.success(t('youtubeTranscript.saved'))
  } catch {
    if (current === generation) MessagePlugin.error(t('youtubeTranscript.saveFailed'))
  } finally { saving.value = false }
}

watch(() => auth.currentTenantId, load, { immediate: true })
</script>

<style scoped lang="less">
.youtube-settings { max-width: 960px; }
.youtube-form { display: flex; flex-direction: column; gap: 20px; }
.setting-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 28px; padding: 18px 0; border-bottom: 1px solid var(--td-component-stroke); }
.setting-info { flex: 1; label { font-weight: 500; color: var(--td-text-color-primary); } p { margin: 6px 0 0; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 1.6; } }
.setting-control { width: 340px; max-width: 100%; flex-shrink: 0; }
.clear-proxy { margin-top: 10px; }
.usage-note { margin: 0; line-height: 1.7; color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.settings-actions { display: flex; justify-content: flex-end; }
@media (max-width: 680px) { .setting-row { flex-wrap: wrap; gap: 14px; } .setting-control { width: 100%; } }
</style>
