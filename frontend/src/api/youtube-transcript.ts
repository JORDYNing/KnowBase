import { get, put } from '@/utils/request'
import type { YouTubeTranscriptConfig, YouTubeTranscriptUpdate } from '@/utils/youtube-transcript'

export type { YouTubeTranscriptConfig, YouTubeTranscriptUpdate }

export function getYouTubeTranscriptConfig() {
  return get('/api/v1/tenants/kv/youtube-transcript-config')
}

export function updateYouTubeTranscriptConfig(config: YouTubeTranscriptUpdate) {
  return put('/api/v1/tenants/kv/youtube-transcript-config', config)
}
