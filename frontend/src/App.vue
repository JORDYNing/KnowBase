<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { MessagePlugin, NotifyPlugin } from 'tdesign-vue-next'
import ProtectedResourcePreview from '@/components/ProtectedResourcePreview.vue'
import ManualKnowledgeEditor from '@/components/manual-knowledge-editor.vue'
import UploadConfirmHost from '@/components/UploadConfirmHost.vue'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { consumePendingTenantSwitchToast } from '@/utils/tenantSwitch'
import { useRoleLabel } from '@/composables/useRoleLabel'
import { notifyLoginSuccess } from '@/utils/loginNotify'
import { renderWorkspaceNotifyContent } from '@/utils/workspaceNotifyContent'

// TDesign locale configs
import enUSConfig from 'tdesign-vue-next/esm/locale/en_US'
import zhCNConfig from 'tdesign-vue-next/esm/locale/zh_CN'
import koKRConfig from 'tdesign-vue-next/esm/locale/ko_KR'
import jaJPConfig from 'tdesign-vue-next/esm/locale/ja_JP'
import ruRUConfig from 'tdesign-vue-next/esm/locale/ru_RU'
import zhTWConfig from 'tdesign-vue-next/esm/locale/zh_TW'

const { locale, t, tm } = useI18n()
const { formatRole, roleIcon } = useRoleLabel()
const router = useRouter()
const authStore = useAuthStore()
const settingsStore = useSettingsStore()

const tdLocaleMap: Record<string, object> = {
  'en-US': enUSConfig,
  'zh-CN': zhCNConfig,
  'zh-TW': zhTWConfig,
  'ko-KR': koKRConfig,
  'ja-JP': jaJPConfig,
  'ru-RU': ruRUConfig,
}

const tdGlobalConfig = computed(() => tdLocaleMap[locale.value] || enUSConfig)

const decodeFeishuResult = (encoded: string) => {
  const normalized = encoded.replace(/-/g, '+').replace(/_/g, '/')
  const padded = normalized + '='.repeat((4 - normalized.length % 4) % 4)
  const binary = window.atob(padded)
  const bytes = Uint8Array.from(binary, char => char.charCodeAt(0))
  return JSON.parse(new TextDecoder().decode(bytes))
}

const clearFeishuCallbackState = (path = '/') => {
  window.history.replaceState({}, document.title, path)
}

// Feishu uses the existing signed callback envelope; all session hydration
// stays in the auth store so pending users never mount tenant resources.
const persistFeishuLoginResponse = async (response: any) => {
  if (!await authStore.applyLoginResponse(response)) throw new Error(t('auth.feishuLoginFailed'))
  await nextTick()
  await router.replace(authStore.hasValidTenant ? '/platform/knowledge-bases' : '/onboarding/workspace')
}

const handleGlobalFeishuCallback = async () => {
  const hash = window.location.hash.startsWith('#') ? window.location.hash.slice(1) : ''
  if (!hash) return

  const params = new URLSearchParams(hash)
  const oidcError = params.get('oidc_error')
  const oidcErrorDescription = params.get('oidc_error_description')
  const oidcResult = params.get('oidc_result')

  if (!oidcError && !oidcResult) return

  if (oidcError) {
    clearFeishuCallbackState('/login')
    await router.replace('/login')
    MessagePlugin.error(oidcErrorDescription || t('auth.feishuLoginFailed'))
    return
  }

  try {
    if (!oidcResult) {
      clearFeishuCallbackState('/login')
      await router.replace('/login')
      MessagePlugin.error(t('auth.feishuLoginFailed'))
      return
    }

    const response = decodeFeishuResult(oidcResult)
    if (response.success) {
      clearFeishuCallbackState('/')
      await persistFeishuLoginResponse(response)
      notifyLoginSuccess(response, t, tm, formatRole, roleIcon)
      return
    }

    clearFeishuCallbackState('/login')
    await router.replace('/login')
    MessagePlugin.error(response.message || t('auth.feishuLoginFailed'))
  } catch (error: any) {
    console.error('Feishu callback handling failed:', error)
    authStore.logout()
    clearFeishuCallbackState('/login')
    await router.replace('/login')
    MessagePlugin.error(error.message || t('auth.feishuLoginFailed'))
  }
}

let updateCheckTimer: ReturnType<typeof setInterval> | null = null

// 切换空间后会 hard reload；切换前 stash 的 toast 这里 consume 并弹出，
// 这样 toast 显示在新页面上，duration 才真正生效。
const showPendingTenantSwitchToast = () => {
  const pending = consumePendingTenantSwitchToast()
  if (!pending) return
  const templateKey = pending.role
    ? 'tenant.switchSuccessContentWithRole'
    : 'tenant.switchSuccessContent'
  // Use tm() not t() — vue-i18n v11's `t()` replaces unspecified named
  // placeholders with empty strings, which would strip {name}/{role}
  // before the chip renderer can split on them. tm() returns the raw
  // message verbatim.
  const rawTemplate = tm(templateKey)
  const template = typeof rawTemplate === 'string' ? rawTemplate : ''
  NotifyPlugin.success({
    title: t('tenant.switchSuccessTitle'),
    content: renderWorkspaceNotifyContent({
      template,
      name: pending.name,
      roleLabel: pending.role,
      roleEnum: pending.roleEnum,
      roleIconName: pending.roleEnum ? roleIcon(pending.roleEnum) : undefined,
    }),
    duration: 6000,
    closeBtn: true,
  })
}

onMounted(() => {
  handleGlobalFeishuCallback()
  showPendingTenantSwitchToast()

  // Auto check for updates on startup
  setTimeout(() => {
    if (settingsStore.isAutoCheckUpdateEnabled) {
      // @ts-ignore
      if (window.go && window.go.main && window.go.main.App && window.go.main.App.AutoCheckForUpdates) {
        // @ts-ignore
        window.go.main.App.AutoCheckForUpdates()
      }
    }
  }, 2000)

  // Periodically check for updates (every 4 hours)
  updateCheckTimer = setInterval(() => {
    if (settingsStore.isAutoCheckUpdateEnabled) {
      // @ts-ignore
      if (window.go && window.go.main && window.go.main.App && window.go.main.App.AutoCheckForUpdates) {
        // @ts-ignore
        window.go.main.App.AutoCheckForUpdates()
      }
    }
  }, 4 * 60 * 60 * 1000)
})

onUnmounted(() => {
  if (updateCheckTimer) {
    clearInterval(updateCheckTimer)
  }
})

</script>
<template>
  <t-config-provider :globalConfig="tdGlobalConfig">
    <div id="app">
      <RouterView />
      <template v-if="authStore.hasValidTenant">
        <ManualKnowledgeEditor />
        <ProtectedResourcePreview />
        <UploadConfirmHost />
      </template>
    </div>
  </t-config-provider>
</template>
<style>
html {
  /* 提示 UA 使用对应配色绘制滚动条等，减少主题切换时的额外重绘 */
  color-scheme: light dark;
}

body,
html,
#app {
  width: 100%;
  height: 100%;
  margin: 0;
  padding: 0;
  font-size: 14px;
  font-family: var(--app-font-family);
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  background: var(--td-bg-color-page);
  color: var(--td-text-color-primary);
}

#app {
  /* 独立合成层，减轻 WebKit 全量重绘时整窗与内容的撕裂感（桌面 WebView 尤其明显） */
  isolation: isolate;
  transform: translateZ(0);
  backface-visibility: hidden;
}
</style>
