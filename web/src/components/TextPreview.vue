<script setup lang="ts">
import { ref, watch } from 'vue'
import { NSpin, NAlert, NButton, NSpace, useMessage } from 'naive-ui'
import { api } from '../api/client'
import { copyText } from '../utils/format'

const props = defineProps<{ path: string }>()
const text = ref('')
const truncated = ref(false)
const loading = ref(true)
const err = ref('')
const msg = useMessage()

async function load() {
  loading.value = true; err.value = ''
  try {
    const r = await api.text(props.path)
    text.value = r.text; truncated.value = r.truncated
  } catch (e: any) { err.value = e.message } finally { loading.value = false }
}
watch(() => props.path, load, { immediate: true })
async function copy() { if (await copyText(text.value)) msg.success('已复制') }
</script>

<template>
  <NSpin :show="loading">
    <NAlert v-if="err" type="error" :bordered="false">{{ err }}</NAlert>
    <template v-else>
      <NSpace justify="end" style="margin-bottom: 8px">
        <span v-if="truncated" style="opacity:.7;font-size:12px">仅显示前 512 KB</span>
        <NButton size="tiny" @click="copy">复制全文</NButton>
      </NSpace>
      <pre class="report mono">{{ text }}</pre>
    </template>
  </NSpin>
</template>
