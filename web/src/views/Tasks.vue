<script setup lang="ts">
import { computed, ref } from 'vue'
import { NRadioGroup, NRadioButton, NEmpty, NButton, NSpace, NTag, useDialog } from 'naive-ui'
import { tasks } from '../stores/tasks'
import TaskCard from '../components/TaskCard.vue'

const filter = ref<'all' | 'active' | 'done' | 'failed'>('all')
const dialog = useDialog()
const list = computed(() => {
  const l = tasks.state.list
  switch (filter.value) {
    case 'active': return l.filter((t) => t.status === 'queued' || t.status === 'running')
    case 'done': return l.filter((t) => t.status === 'done')
    case 'failed': return l.filter((t) => t.status === 'failed' || t.status === 'cancelled')
    default: return l
  }
})
function clearFinished() {
  const fin = tasks.state.list.filter((t) => t.status !== 'queued' && t.status !== 'running')
  if (!fin.length) return
  dialog.warning({
    title: '清理已结束任务', content: `删除 ${fin.length} 条记录？不会删除已生成的截图和种子文件。`,
    positiveText: '清理', negativeText: '取消',
    onPositiveClick: async () => { for (const t of fin) await tasks.remove(t.id) },
  })
}
</script>

<template>
  <div>
    <div class="page-head">
      <h2>任务 <NTag v-if="!tasks.state.connected" size="small" type="warning" :bordered="false">事件流断开，重连中</NTag></h2>
      <NSpace align="center">
        <NRadioGroup v-model:value="filter" size="small">
          <NRadioButton value="all">全部</NRadioButton>
          <NRadioButton value="active">进行中 ({{ tasks.active.value.length }})</NRadioButton>
          <NRadioButton value="done">完成</NRadioButton>
          <NRadioButton value="failed">失败</NRadioButton>
        </NRadioGroup>
        <NButton size="small" @click="clearFinished">清理已结束</NButton>
      </NSpace>
    </div>
    <NEmpty v-if="!list.length" description="没有任务" style="margin-top: 60px" />
    <TaskCard v-for="t in list" :key="t.id" :task="t" />
  </div>
</template>
