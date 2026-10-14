<template>
  <span class="resource-origin-badge" :class="variantClass" :title="tooltipText">
    <t-icon :name="iconName" size="12px" class="badge-icon" />
    <span class="badge-text">{{ displayText }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'

const props = withDefaults(defineProps<{
  variant: 'mine' | 'tenant' | 'creator'
  creatorName?: string
}>(), { creatorName: '' })
const { t } = useI18n()
const authStore = useAuthStore()
const variantClass = computed(() => `origin-${props.variant}`)
const iconName = computed(() => props.variant === 'tenant' ? 'usergroup' : 'user')
const displayText = computed(() => {
  if (props.variant === 'mine') return t('resourceOrigin.mine')
  if (props.variant === 'creator') return props.creatorName || t('resourceOrigin.tenant')
  return authStore.currentTenantName || t('resourceOrigin.tenant')
})
const tooltipText = computed(() => {
  if (props.variant === 'mine') return t('resourceOrigin.mineTooltip')
  return props.creatorName ? t('resourceOrigin.tenantTooltipWithCreator', { creator: props.creatorName }) : t('resourceOrigin.tenantTooltip')
})
</script>

<style scoped lang="less">
.resource-origin-badge {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 6px;
  border-radius: var(--app-radius-md);
  font-size: var(--app-text-xs);
  line-height: 1.4;
  font-weight: 500;
  max-width: 140px;

  .badge-icon {
    flex-shrink: 0;
  }

  .badge-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &.origin-mine {
    color: var(--td-brand-color);
    background: var(--td-success-color-light);
  }

  &.origin-tenant {
    color: var(--td-text-color-secondary);
    background: var(--td-bg-color-secondarycontainer);
  }

  &.origin-creator {
    color: var(--td-text-color-secondary);
    background: var(--td-bg-color-secondarycontainer);
  }




}
</style>
