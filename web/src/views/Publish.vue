<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  NCard, NForm, NFormItem, NInput, NInputNumber, NSwitch, NSelect, NButton, NSpace, NGrid, NGi, NAlert, NTag,
  NSteps, NStep, NDivider, NCheckbox, NSpin, useMessage,
} from 'naive-ui'
import { api } from '../api/client'
import type { Entry, Playlist, Task, PipelineResult, Template, BBData } from '../api/types'
import { bytes, duration, basename, copyText } from '../utils/format'
import { tasks } from '../stores/tasks'
import { session } from '../stores/session'
import TaskCard from '../components/TaskCard.vue'

const route = useRoute()
const msg = useMessage()

const path = ref<string>((route.query.p as string) || '')
const entry = ref<Entry | null>(null)
const checking = ref(false)
const isDisc = computed(() => entry.value && (entry.value.kind === 'bdmv' || entry.value.kind === 'iso'))
const playlists = ref<Playlist[]>([])
const playlist = ref('')
const plLoading = ref(false)

const opt = ref({
  info: true,
  shot: true, count: 0, language: '', upload: true, host: '', compress: null as boolean | null,
  torrent: true, announce: '', source: '', private: null as boolean | null, pieceSize: 0, comment: '',
})
const hosts = [{ label: '默认（设置中配置）', value: '' }, { label: 'picgo.net', value: 'picgo' }, { label: 'pixhost', value: 'pixhost' }]
const langs = [
  { label: '按设置（默认字幕轨）', value: '' }, { label: '不烧录字幕', value: 'none' }, { label: '默认轨', value: 'default' },
  { label: '中文 (chi/zho)', value: 'chi' }, { label: '英文 (eng)', value: 'eng' }, { label: '日文 (jpn)', value: 'jpn' },
]
const pieceSizes = [{ label: '自动', value: 0 }, ...[16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26].map((e) => ({ label: bytes(2 ** e), value: e }))]

async function check() {
  if (!path.value) { entry.value = null; return }
  checking.value = true
  try {
    entry.value = await api.stat(path.value)
    playlists.value = []; playlist.value = ''
    if (isDisc.value) loadPlaylists()
  } catch (e: any) { entry.value = null; msg.error(e.message) } finally { checking.value = false }
}
async function loadPlaylists() {
  plLoading.value = true
  try {
    const r = await api.playlists(path.value)
    playlists.value = r.playlists.filter((p) => p.IsValid).sort((a, b) => b.SizeBytes - a.SizeBytes)
  } catch (e: any) { msg.warning('读取播放列表失败：' + e.message) } finally { plLoading.value = false }
}
const plOptions = computed(() => [
  { label: '自动（最大播放列表）', value: '' },
  ...playlists.value.map((p) => ({ label: `${p.Name} · ${duration(p.LengthSeconds)} · ${bytes(p.SizeBytes)}${p.HasHiddenTracks ? ' · 含隐藏轨' : ''}`, value: p.Name })),
])
onMounted(check)
watch(() => route.query.p, (p) => { if (p && p !== path.value) { path.value = p as string; check() } })

// ---- run ----
const task = ref<Task | null>(null)
const running = computed(() => task.value && (task.value.status === 'queued' || task.value.status === 'running'))
const result = computed<PipelineResult | null>(() => (task.value?.status === 'done' || task.value?.status === 'failed') ? (task.value.result as PipelineResult) : null)
const step = computed(() => (result.value ? 2 : task.value ? 1 : 0))

async function run() {
  if (!entry.value) return
  const o = opt.value
  try {
    const t = await api.pipeline({
      Path: path.value, Info: o.info, Playlist: playlist.value,
      Screenshot: o.shot ? { Count: o.count || undefined, Language: o.language, Upload: o.upload, Host: o.host, Compress: o.compress } : null,
      Torrent: o.torrent ? { Announce: o.announce, Source: o.source, Private: o.private, PieceSize: o.pieceSize, Comment: o.comment } : null,
    })
    task.value = t
    const fin = await tasks.wait(t.id)
    task.value = fin
    if (fin.status === 'done') { msg.success('发布流水线完成'); render() }
    else msg.error(fin.error || '任务未成功')
  } catch (e: any) { msg.error(e.message) }
}
watch(() => task.value && tasks.get(task.value.id), (t) => { if (t) task.value = t })

// ---- template ----
const templates = ref<Template[]>([])
const tplName = ref('default')
const title = ref('')
const bbcode = ref('')
const rendering = ref(false)
onMounted(async () => { try { templates.value = await api.templates() } catch {} })
const data = computed<BBData>(() => {
  const r = result.value || {}
  const shots = r.screenshot?.shots ?? []
  return {
    Title: title.value || basename(path.value),
    MediaInfo: r.mediainfo ?? '',
    BDInfo: r.bdinfo?.quickSummary ?? '',
    BDInfoFull: r.bdinfo?.report ?? '',
    Images: shots.map((s) => s.url).filter(Boolean),
    Torrent: r.torrent?.web ? location.origin + r.torrent.web : '',
    Source: path.value,
  }
})
async function render() {
  rendering.value = true
  try { bbcode.value = (await api.render(tplName.value, data.value)).text } catch (e: any) { msg.error(e.message) } finally { rendering.value = false }
}
watch([tplName, title], () => { if (result.value) render() })
async function copy() { if (await copyText(bbcode.value)) msg.success('BBCode 已复制') }
function reset() { task.value = null; bbcode.value = '' }
</script>

<template>
  <div>
    <div class="page-head"><h2>发布向导</h2>
      <NSteps :current="step + 1" size="small" style="max-width: 520px">
        <NStep title="选择 & 配置" /><NStep title="处理中" /><NStep title="拼装 BBCode" />
      </NSteps>
    </div>

    <NGrid :cols="12" :x-gap="16" :y-gap="16" item-responsive responsive="screen">
      <NGi span="12 l:5">
        <NCard title="来源" size="small">
          <NInput v-model:value="path" placeholder="/data/Movie.2024.BluRay/ 或 文件路径" @blur="check" @keyup.enter="check" :loading="checking">
            <template #suffix><NButton text size="tiny" @click="check">检查</NButton></template>
          </NInput>
          <div v-if="entry" style="margin-top: 8px">
            <NSpace size="small">
              <NTag size="small" :bordered="false" type="info">{{ entry.kind.toUpperCase() }}</NTag>
              <NTag size="small" :bordered="false">{{ bytes(entry.size) }}</NTag>
            </NSpace>
            <NFormItem v-if="isDisc" label="播放列表" style="margin-top: 10px" :show-feedback="false">
              <NSelect v-model:value="playlist" :options="plOptions" :loading="plLoading" />
            </NFormItem>
          </div>
          <NAlert v-else-if="path && !checking" type="warning" :bordered="false" style="margin-top: 8px">路径无效，请从“文件”页选择</NAlert>
        </NCard>

        <NCard title="步骤" size="small" style="margin-top: 16px">
          <NForm label-placement="left" label-width="90" size="small" :show-feedback="false">
            <NCheckbox v-model:checked="opt.info" style="margin-bottom: 8px">{{ isDisc ? 'BDInfo 扫描' : 'MediaInfo' }}</NCheckbox>
            <NDivider style="margin: 8px 0" />
            <NCheckbox v-model:checked="opt.shot" :disabled="!session.tools?.ffmpeg.found">截图{{ session.tools?.ffmpeg.found ? '' : '（未找到 ffmpeg）' }}</NCheckbox>
            <template v-if="opt.shot">
              <NFormItem label="数量" style="margin-top: 8px"><NInputNumber v-model:value="opt.count" :min="0" :max="20" placeholder="默认" style="width: 120px" /></NFormItem>
              <NFormItem label="字幕"><NSelect v-model:value="opt.language" :options="langs" /></NFormItem>
              <NFormItem label="上传图床"><NSwitch v-model:value="opt.upload" /><NSelect v-if="opt.upload" v-model:value="opt.host" :options="hosts" style="margin-left: 10px; width: 180px" /></NFormItem>
            </template>
            <NDivider style="margin: 8px 0" />
            <NCheckbox v-model:checked="opt.torrent">制种</NCheckbox>
            <template v-if="opt.torrent">
              <NFormItem label="Tracker" style="margin-top: 8px"><NInput v-model:value="opt.announce" placeholder="留空用设置里的默认" /></NFormItem>
              <NFormItem label="Source"><NInput v-model:value="opt.source" placeholder="留空用默认" /></NFormItem>
              <NFormItem label="分块"><NSelect v-model:value="opt.pieceSize" :options="pieceSizes" style="width: 160px" /></NFormItem>
              <NFormItem label="Comment"><NInput v-model:value="opt.comment" /></NFormItem>
            </template>
          </NForm>
          <NSpace justify="end" style="margin-top: 12px">
            <NButton v-if="task" @click="reset">重置</NButton>
            <NButton type="primary" :disabled="!entry || !!running" :loading="!!running" @click="run">开始</NButton>
          </NSpace>
        </NCard>
      </NGi>

      <NGi span="12 l:7">
        <NCard v-if="task" size="small" :title="running ? '处理中' : '处理结果'">
          <TaskCard :task="task" />
        </NCard>
        <NCard v-if="result" size="small" title="BBCode" style="margin-top: 16px">
          <NForm label-placement="left" label-width="60" size="small" :show-feedback="false" inline>
            <NFormItem label="模板"><NSelect v-model:value="tplName" :options="templates.map((t) => ({ label: t.name, value: t.name }))" style="width: 180px" /></NFormItem>
            <NFormItem label="标题"><NInput v-model:value="title" :placeholder="basename(path)" style="width: 280px" /></NFormItem>
          </NForm>
          <NSpin :show="rendering">
            <NInput v-model:value="bbcode" type="textarea" :autosize="{ minRows: 10, maxRows: 28 }" class="mono" style="margin-top: 10px" />
          </NSpin>
          <NSpace justify="end" style="margin-top: 10px">
            <NButton @click="render">重新渲染</NButton>
            <NButton type="primary" @click="copy">复制 BBCode</NButton>
          </NSpace>
        </NCard>
        <NCard v-if="!task" size="small">
          <NAlert type="info" :bordered="false">
            在左侧选择来源并勾选步骤，点击“开始”。流水线会依次执行信息提取、截图上传、制种，完成后在此拼装 BBCode。
            你也可以在“文件”页对单个条目右键单独执行某一步。
          </NAlert>
        </NCard>
      </NGi>
    </NGrid>
  </div>
</template>
