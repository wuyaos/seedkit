<script setup lang="ts">
import { computed, h, onMounted, ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  NButton, NButtonGroup, NDataTable, NBreadcrumb, NBreadcrumbItem, NSpace, NIcon, NInput, NDropdown, NTag,
  NModal, NCard, NEmpty, NTooltip, useMessage, useDialog, type DataTableColumns, type DropdownOption,
} from 'naive-ui'
import {
  FolderOutline, FilmOutline, ImageOutline, DocumentOutline, ArchiveOutline, DiscOutline, MagnetOutline,
  RefreshOutline, AddOutline, TrashOutline, CreateOutline, ArrowUpOutline, DownloadOutline,
  CopyOutline, CutOutline, EllipsisHorizontal, InformationCircleOutline, CameraOutline, RocketOutline,
} from '@vicons/ionicons5'
import { api } from '../api/client'
import type { Entry, Root } from '../api/types'
import { bytes, dateTime, dirname, basename } from '../utils/format'
import { tasks } from '../stores/tasks'
import TextPreview from '../components/TextPreview.vue'

const router = useRouter()
const route = useRoute()
const msg = useMessage()
const dialog = useDialog()

const roots = ref<Root[]>([])
const path = ref<string>((route.query.p as string) || '')
const entries = ref<Entry[]>([])
const loading = ref(false)
const checked = ref<string[]>([])
const filter = ref('')
const clipboard = ref<{ paths: string[]; move: boolean } | null>(null)

const crumbs = computed(() => {
  const parts = path.value.split('/').filter(Boolean)
  return parts.map((p, i) => ({ name: p, path: '/' + parts.slice(0, i + 1).join('/') }))
})
const filtered = computed(() => {
  const f = filter.value.trim().toLowerCase()
  const list = f ? entries.value.filter((e) => e.name.toLowerCase().includes(f)) : entries.value
  return [...list].sort((a, b) => (a.isDir === b.isDir ? a.name.localeCompare(b.name, 'zh') : a.isDir ? -1 : 1))
})
const selected = computed(() => entries.value.filter((e) => checked.value.includes(e.path)))

async function load() {
  loading.value = true
  checked.value = []
  try {
    if (!roots.value.length) roots.value = await api.roots()
    if (!path.value) {
      entries.value = roots.value.map((r) => ({ name: r.name, path: '/' + r.name, isDir: true, size: 0, modTime: '', kind: 'dir' }))
    } else {
      entries.value = await api.list(path.value)
    }
  } catch (e: any) {
    msg.error(e.message)
    if (path.value) { path.value = ''; return load() }
  } finally { loading.value = false }
}
function go(p: string) {
  path.value = p
  router.replace({ query: p ? { p } : {} })
}
watch(path, load)
onMounted(load)

const kindIcon: Record<string, any> = {
  dir: FolderOutline, video: FilmOutline, image: ImageOutline, text: DocumentOutline, archive: ArchiveOutline,
  bdmv: DiscOutline, iso: DiscOutline, torrent: MagnetOutline, other: DocumentOutline,
}
const kindColor: Record<string, string> = { dir: '#E9B949', video: '#5B8DEF', bdmv: '#9B6DFF', iso: '#9B6DFF', image: '#3DBB7A', torrent: '#F0734A' }

const columns: DataTableColumns<Entry> = [
  { type: 'selection' },
  {
    title: '名称', key: 'name', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('div', { class: 'name-cell', onClick: () => open(row) }, [
      h(NIcon, { size: 18, color: kindColor[row.kind] ?? '#999', style: 'vertical-align:-4px;margin-right:8px' }, { default: () => h(kindIcon[row.kind] ?? DocumentOutline) }),
      h('span', { class: row.isDir || row.kind === 'bdmv' ? 'link' : '' }, row.name),
      row.kind === 'bdmv' || row.kind === 'iso' ? h(NTag, { size: 'tiny', type: 'info', bordered: false, style: 'margin-left:8px' }, { default: () => row.kind.toUpperCase() }) : null,
    ]),
  },
  { title: '大小', key: 'size', width: 110, align: 'right', sorter: (a, b) => a.size - b.size, render: (r) => (r.isDir && r.kind === 'dir' && !r.size ? '' : bytes(r.size)) },
  { title: '修改时间', key: 'modTime', width: 170, sorter: 'default', render: (r) => dateTime(r.modTime) },
  {
    title: '', key: 'actions', width: 56, align: 'center',
    render: (row) => h(NDropdown, { options: rowMenu(row), trigger: 'click', onSelect: (k: string) => act(k, [row]) },
      { default: () => h(NButton, { quaternary: true, circle: true, size: 'small' }, { icon: () => h(NIcon, null, { default: () => h(EllipsisHorizontal) }) }) }),
  },
]
const mediaKinds = ['video', 'bdmv', 'iso', 'dir']
function rowMenu(row: Entry): DropdownOption[] {
  const isMedia = mediaKinds.includes(row.kind) && row.path.split('/').length > 2
  return [
    ...(isMedia ? [
      { key: 'publish', label: '发布向导', icon: () => h(NIcon, null, { default: () => h(RocketOutline) }) },
      { key: 'info', label: row.kind === 'video' ? 'MediaInfo' : 'BDInfo', icon: () => h(NIcon, null, { default: () => h(InformationCircleOutline) }) },
      { key: 'shot', label: '截图', icon: () => h(NIcon, null, { default: () => h(CameraOutline) }) },
      { key: 'torrent', label: '制种', icon: () => h(NIcon, null, { default: () => h(MagnetOutline) }) },
      { type: 'divider', key: 'd1' },
    ] : []),
    ...(row.kind === 'text' || row.kind === 'torrent' ? [{ key: 'view', label: '查看', icon: () => h(NIcon, null, { default: () => h(DocumentOutline) }) }] : []),
    { key: 'download', label: row.isDir ? '下载 (zip)' : '下载', icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }) },
    { key: 'rename', label: '重命名', icon: () => h(NIcon, null, { default: () => h(CreateOutline) }) },
    { key: 'copy', label: '复制', icon: () => h(NIcon, null, { default: () => h(CopyOutline) }) },
    { key: 'cut', label: '移动', icon: () => h(NIcon, null, { default: () => h(CutOutline) }) },
    { key: 'delete', label: '删除', icon: () => h(NIcon, null, { default: () => h(TrashOutline) }) },
  ]
}

const preview = ref<{ path: string } | null>(null)
function open(row: Entry) {
  if (row.isDir && row.kind !== 'bdmv') go(row.path)
  else if (row.kind === 'bdmv') router.push({ path: '/publish', query: { p: row.path } })
  else if (row.kind === 'text' || row.kind === 'torrent') preview.value = { path: row.path }
  else if (row.kind === 'video' || row.kind === 'iso') router.push({ path: '/publish', query: { p: row.path } })
}

async function act(key: string, rows: Entry[]) {
  const one = rows[0]
  switch (key) {
    case 'publish': router.push({ path: '/publish', query: { p: one.path } }); break
    case 'view': preview.value = { path: one.path }; break
    case 'download': window.open(api.downloadURL(one.path), '_blank'); break
    case 'info': {
      const t = one.kind === 'video' ? await api.mediainfo(one.path) : await api.bdinfo(one.path)
      msg.success(`已提交任务：${t.title}`); break
    }
    case 'shot': { const t = await api.screenshot(one.path, {}); msg.success(`已提交任务：${t.title}`); break }
    case 'torrent': { const t = await api.torrent(one.path, {}); msg.success(`已提交任务：${t.title}`); break }
    case 'copy': clipboard.value = { paths: rows.map((r) => r.path), move: false }; msg.info(`已复制 ${rows.length} 项，进入目标目录后点击“粘贴”`); break
    case 'cut': clipboard.value = { paths: rows.map((r) => r.path), move: true }; msg.info(`已剪切 ${rows.length} 项，进入目标目录后点击“粘贴”`); break
    case 'rename': renameDlg(one); break
    case 'delete': deleteDlg(rows); break
  }
}
function renameDlg(row: Entry) {
  const name = ref(row.name)
  dialog.create({
    title: '重命名', positiveText: '确定', negativeText: '取消',
    content: () => h(NInput, { value: name.value, 'onUpdate:value': (v: string) => (name.value = v), autofocus: true }),
    onPositiveClick: async () => {
      if (!name.value || name.value === row.name) return
      try { await api.rename(row.path, name.value); load() } catch (e: any) { msg.error(e.message); return false }
    },
  })
}
function deleteDlg(rows: Entry[]) {
  dialog.warning({
    title: '确认删除', positiveText: '删除', negativeText: '取消',
    content: `将永久删除 ${rows.length} 项：${rows.slice(0, 5).map((r) => r.name).join('、')}${rows.length > 5 ? ' …' : ''}`,
    onPositiveClick: async () => {
      try { await api.remove(rows.map((r) => r.path)); msg.success('已删除'); load() } catch (e: any) { msg.error(e.message); return false }
    },
  })
}
function mkdirDlg() {
  const name = ref('')
  dialog.create({
    title: '新建文件夹', positiveText: '创建', negativeText: '取消',
    content: () => h(NInput, { value: name.value, 'onUpdate:value': (v: string) => (name.value = v), autofocus: true, placeholder: '文件夹名' }),
    onPositiveClick: async () => {
      if (!name.value) return false
      try { await api.mkdir(path.value.replace(/\/$/, '') + '/' + name.value); load() } catch (e: any) { msg.error(e.message); return false }
    },
  })
}
async function paste() {
  const c = clipboard.value
  if (!c || !path.value) return
  try {
    const t = c.move ? await api.move(c.paths, path.value) : await api.copy(c.paths, path.value)
    msg.success(`已提交：${t.title}`)
    clipboard.value = null
    tasks.wait(t.id).then(load)
  } catch (e: any) { msg.error(e.message) }
}
const canPaste = computed(() => clipboard.value && path.value && !clipboard.value.paths.some((p) => dirname(p) === path.value))
</script>

<template>
  <div>
    <div class="page-head">
      <NBreadcrumb>
        <NBreadcrumbItem @click="go('')">根目录</NBreadcrumbItem>
        <NBreadcrumbItem v-for="c in crumbs" :key="c.path" @click="go(c.path)">{{ c.name }}</NBreadcrumbItem>
      </NBreadcrumb>
      <NSpace size="small" align="center">
        <NInput v-model:value="filter" size="small" clearable placeholder="筛选当前目录" style="width: 200px" />
        <NButtonGroup size="small">
          <NTooltip><template #trigger><NButton :disabled="!path" @click="go(dirname(path) === '/' ? '' : dirname(path))"><template #icon><NIcon><ArrowUpOutline /></NIcon></template></NButton></template>上级目录</NTooltip>
          <NTooltip><template #trigger><NButton :loading="loading" @click="load"><template #icon><NIcon><RefreshOutline /></NIcon></template></NButton></template>刷新</NTooltip>
          <NTooltip><template #trigger><NButton :disabled="!path" @click="mkdirDlg"><template #icon><NIcon><AddOutline /></NIcon></template></NButton></template>新建文件夹</NTooltip>
        </NButtonGroup>
        <NButton v-if="clipboard" size="small" type="primary" :disabled="!canPaste" @click="paste">
          粘贴 {{ clipboard.paths.length }} 项（{{ clipboard.move ? '移动' : '复制' }}）
        </NButton>
        <NButton v-if="clipboard" size="small" quaternary @click="clipboard = null">清空剪贴板</NButton>
      </NSpace>
    </div>

    <NSpace v-if="selected.length" size="small" style="margin-bottom: 10px" align="center">
      <NTag type="info" :bordered="false">已选 {{ selected.length }} 项</NTag>
      <NButton size="small" @click="act('copy', selected)">复制</NButton>
      <NButton size="small" @click="act('cut', selected)">移动</NButton>
      <NButton size="small" type="error" ghost @click="act('delete', selected)">删除</NButton>
    </NSpace>

    <NDataTable :columns="columns" :data="filtered" :loading="loading" :row-key="(r: Entry) => r.path" v-model:checked-row-keys="checked"
      size="small" :bordered="false" :single-line="false" striped :max-height="'calc(100vh - 160px)'" virtual-scroll :pagination="false">
      <template #empty><NEmpty :description="path ? '空目录' : '未配置根目录，请到“设置”添加'" /></template>
    </NDataTable>

    <NModal :show="!!preview" @update:show="(v: boolean) => { if (!v) preview = null }" :mask-closable="true">
      <NCard v-if="preview" :title="basename(preview.path)" style="width: 860px; max-width: 95vw" closable @close="preview = null">
        <TextPreview :path="preview.path" />
      </NCard>
    </NModal>
  </div>
</template>

<style scoped>
.name-cell { cursor: default; }
.name-cell .link { cursor: pointer; }
.name-cell .link:hover { text-decoration: underline; }
:deep(.n-breadcrumb-item__link) { cursor: pointer; }
</style>
