<template>
  <div class="resource-toolbar">
    <div v-if="!hideScopes" class="scope-filters">
      <div class="category-tabs" role="group" :aria-label="$t('listSpaceSidebar.all')">
        <button v-for="item in scopes" :key="item.value" type="button"
          :aria-pressed="modelValue === item.value" @click="$emit('update:modelValue', item.value)">
          {{ item.label }}
          <span v-if="item.count !== undefined" class="scope-count">{{ item.count }}</span>
        </button>
      </div>
    </div>
    <t-input :model-value="query" class="search-input" :placeholder="$t('menu.search')"
      :aria-label="$t('menu.search')" clearable @update:model-value="(value: unknown) => $emit('update:query', String(value ?? ''))">
      <template #prefix-icon><t-icon name="search" size="16px" /></template>
    </t-input>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(defineProps<{
  modelValue: string
  query: string
  hideScopes?: boolean
  countAll?: number
  countMine?: number
  countFavorites?: number
  countRecents?: number
}>(), { hideScopes: false })
defineEmits<{
  'update:modelValue': [value: string]
  'update:query': [value: string]
}>()
const { t } = useI18n()
const scopes = computed(() => [
  { value: 'all', label: t('listSpaceSidebar.all'), count: props.countAll },
  { value: 'favorites', label: t('listSpaceSidebar.favorites'), count: props.countFavorites },
  { value: 'recents', label: t('listSpaceSidebar.recents'), count: props.countRecents },
])
</script>

<style scoped lang="less">
@import (reference) '@/components/css/artifact-filter-tabs.less';
.resource-toolbar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--app-space-3);
  padding-bottom: var(--app-space-3);
  border-bottom: 1px solid var(--td-component-stroke);
}
.scope-filters {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--app-space-3);
  min-width: 0;
}
.category-tabs { .artifact-filter-tabs(); }
.scope-count { margin-left: 5px; font-size: var(--app-text-xs); font-variant-numeric: tabular-nums; }
.space-filter { width: 180px; }
.space-filter-icon-active { color: var(--td-brand-color); }
.search-input { width: 260px; max-width: 100%; margin-left: auto; }
@media (max-width: 720px) {
  .search-input { width: 100%; }
  .space-filter { max-width: 100%; }
}
</style>
