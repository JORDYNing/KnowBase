export interface YouTubeTranscriptConfig {
  enabled: boolean
  knowledge_base_id: string
  proxy_url?: string
  proxy_configured?: boolean
}

export interface YouTubeTranscriptUpdate {
  enabled: boolean
  knowledge_base_id: string
  proxy_url?: string
  clear_proxy?: boolean
}

// The password input is never populated from the masked response. An empty
// edit preserves the stored proxy; clearing is a separate explicit operation.
export function buildYouTubeTranscriptUpdate(
  enabled: boolean, knowledgeBaseId: string, proxyInput: string, clearProxy: boolean,
): YouTubeTranscriptUpdate {
  const payload: YouTubeTranscriptUpdate = { enabled, knowledge_base_id: knowledgeBaseId }
  if (clearProxy) payload.clear_proxy = true
  else if (proxyInput.trim()) payload.proxy_url = proxyInput.trim()
  return payload
}

export interface YouTubeKnowledgeBaseOption {
  id: string
  name: string
  type?: string
  tenant_id?: number | string
}

export function youtubeKnowledgeBaseOptions(items: YouTubeKnowledgeBaseOption[], tenantId: number | string | null | undefined) {
  return items.filter(kb => (!kb.type || kb.type === 'document')
    && (kb.tenant_id == null || String(kb.tenant_id) === String(tenantId)))
    .map(kb => ({ label: kb.name, value: kb.id }))
}

export function safeYouTubeResultLink(value: unknown, internal = false): string {
  if (typeof value !== 'string' || !value || /[\\\u0000-\u0020]/.test(value)) return ''
  if (internal && value.startsWith('/') && !value.startsWith('//')) return value
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password ? url.href : ''
  } catch { return '' }
}
