<template>
  <span class="resource-icon" aria-hidden="true" :style="iconStyle" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import knowledgeIcon from '@/assets/img/zhishiku.svg'
import agentIcon from '@/assets/img/agent.svg'
import artifactIcon from '@/assets/img/artifact.svg'
import toolboxIcon from '@/assets/img/toolbox.svg'

const props = withDefaults(defineProps<{
  type: 'knowledge' | 'agent' | 'artifact' | 'toolbox'
  size?: number
}>(), { size: 20 })

// Reuse the navigation assets while inheriting the surrounding theme color.
const icons = {
  knowledge: knowledgeIcon,
  agent: agentIcon,
  artifact: artifactIcon,
  toolbox: toolboxIcon,
}
const iconStyle = computed(() => ({
  width: `${props.size}px`,
  height: `${props.size}px`,
  maskImage: `url("${icons[props.type]}")`,
}))
</script>

<style scoped>
.resource-icon {
  display: inline-block;
  flex-shrink: 0;
  vertical-align: middle;
  background-color: currentColor;
  mask-repeat: no-repeat;
  mask-position: center;
  mask-size: contain;
}
</style>
