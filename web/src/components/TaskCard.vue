<script setup lang="ts">
import { computed, ref } from 'vue'
import { NCard, NProgress, NTag, NButton, NSpace, NCollapse, NCollapseItem, NIcon, NTooltip, NPopconfirm } from 'naive-ui'
import { CloseOutline, TrashOutline } from '@vicons/ionicons5'
import type { Task } from '../api/types'
import { relTime, dateTime } from '../utils/format'
import { tasks } from '../stores/tasks'
import TaskResult from './TaskResult.vue'

const props = defineProps<{ task: Task; compact?: boolean }>()
const t = computed(() => props.task)
const open = ref(false)
const typeMap: Record<string, 'default' | 'info' | 'success' | 'error' | 'warning'> = {
  queued: 'default', running: 'info', done: 'success', failed: 'error', cancelled: 'warning',
}
const label: Record<string, string> = { queued: '排队中', running: '运行中', done: '完成', failed: '失败', cancelled: '已取消' }
const kindLabel: Record<string, string> = {
  mediainfo: 'MediaInfo', bdinfo: 'BDInfo', screenshot: '截图', torrent: '制种', fileop: '文件', upload: '上传', pipeline: '发布',
}
const active = computed(() => t.value.status === 'queued' || t.value.status === 'running')
const pct = computed(() => Math.round((t.value.progress < 0 ? 0 : t.value.progress) * 100))
const elapsed = computed(() => {
  if (!t.value.started) return ''
  const end = t.value.finished ? new Date(t.value.finished).getTime() : Date.now()
  return Math.round((end - new Date(t.value.started).getTime()) / 1000) + ' 秒'
})
</script>

<template>
  <NCard size="small" :bordered="true" class="task-card" :class="{ active }">
    <div class="row">
      <NTag size="small" :bordered="false" :type="typeMap[t.status]">{{ label[t.status] }}</NTag>
      <NTag size="small" :bordered="false">{{ kindLabel[t.kind] ?? t.kind }}</NTag>
      <span class="title" :title="t.title">{{ t.title }}</span>
      <span class="meta">
        <NTooltip><template #trigger><span>{{ relTime(t.created) }}</span></template>{{ dateTime(t.created) }}</NTooltip>
        <span v-if="elapsed"> · {{ elapsed }}</span>
      </span>
      <NSpace size="small" :wrap="false">
        <NButton v-if="active" size="tiny" quaternary circle @click="tasks.cancel(t.id)"><template #icon><NIcon><CloseOutline /></NIcon></template></NButton>
        <NPopconfirm v-else @positive-click="tasks.remove(t.id)" positive-text="删除" negative-text="取消">
          <template #trigger><NButton size="tiny" quaternary circle><template #icon><NIcon><TrashOutline /></NIcon></template></NButton></template>
          删除此任务记录？
        </NPopconfirm>
      </NSpace>
    </div>
    <NProgress v-if="active" :percentage="pct" :processing="t.status === 'running'" :show-indicator="t.progress >= 0" :height="6" style="margin: 6px 0 2px" />
    <div v-if="t.message || t.error" class="msg" :class="{ err: t.error }">{{ t.error || t.message }}</div>
    <NCollapse v-if="!compact && (t.result || t.log?.length)" :default-expanded-names="t.status === 'done' ? [] : ['log']" arrow-placement="right" style="margin-top: 6px">
      <NCollapseItem v-if="t.result" title="结果" name="result"><TaskResult :task="t" /></NCollapseItem>
      <NCollapseItem v-if="t.log?.length" title="日志" name="log">
        <pre class="report mono" style="max-height: 240px">{{ t.log.join('\n') }}</pre>
      </NCollapseItem>
    </NCollapse>
  </NCard>
</template>

<style scoped>
.task-card { margin-bottom: 8px; }
.task-card.active { border-color: #5B8DEF55; }
.row { display: flex; align-items: center; gap: 8px; }
.title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
.meta { font-size: 12px; opacity: .65; white-space: nowrap; display: inline-flex; align-items: center; gap: 4px; }
.meta :deep(span) { text-decoration: none; }
.msg { font-size: 12px; opacity: .75; margin-top: 4px; word-break: break-all; }
.msg.err { color: #e06c6c; opacity: 1; }
</style>
