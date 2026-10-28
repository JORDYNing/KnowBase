<template>
  <div class="youtube-result">
    <div class="result-heading"><t-icon name="video" size="20px" /><strong>{{ data.title || $t('youtubeTranscript.title') }}</strong></div>
    <p class="result-status" role="status">{{ $t(statusKey) }}</p>
    <dl>
      <template v-if="data.language"><dt>{{ $t('youtubeTranscript.language') }}</dt><dd>{{ data.language }}</dd></template>
      <template v-if="data.knowledge_base_name"><dt>{{ $t('youtubeTranscript.knowledgeBase') }}</dt><dd>{{ data.knowledge_base_name }}</dd></template>
    </dl>
    <div class="result-links">
      <a v-if="documentLink" :href="documentLink" target="_blank" rel="noopener noreferrer">{{ $t('youtubeTranscript.document') }}</a>
      <a v-if="videoLink" :href="videoLink" target="_blank" rel="noopener noreferrer">{{ $t('youtubeTranscript.video') }}</a>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { YouTubeTranscriptData } from '@/types/tool-results'
import { safeYouTubeResultLink } from '@/utils/youtube-transcript'
const props = defineProps<{ data: YouTubeTranscriptData }>()
const videoLink = computed(() => safeYouTubeResultLink(props.data.url))
const documentLink = computed(() => safeYouTubeResultLink(props.data.document_url, true))
const statusKey = computed(() => {
  if (props.data.already_exists) return 'youtubeTranscript.alreadyExists'
  if (props.data.status === 'failed') return 'youtubeTranscript.processingFailed'
  if (['completed', 'ready', 'success'].includes(props.data.status || '')) return 'youtubeTranscript.ready'
  return 'youtubeTranscript.savedProcessing'
})
</script>
<style scoped lang="less">
.youtube-result { padding: 16px; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-sm); background: var(--td-bg-color-container); }
.result-heading { display: flex; align-items: flex-start; gap: 8px; overflow-wrap: anywhere; strong { font-weight: 500; } }
.result-status { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
dl { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 16px; font-size: var(--app-text-sm); }
dt { color: var(--td-text-color-secondary); } dd { margin: 0; overflow-wrap: anywhere; }
.result-links { display: flex; gap: 18px; flex-wrap: wrap; a { color: var(--td-brand-color); text-decoration: none; &:hover { text-decoration: underline; } } }
</style>
