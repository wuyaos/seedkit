<script setup lang="ts">
import { onMounted, ref, h } from 'vue'
import { NCard, NList, NListItem, NButton, NInput, NSpace, NGrid, NGi, NEmpty, NAlert, NTag, NPopconfirm, useMessage, useDialog } from 'naive-ui'
import { api } from '../api/client'
import type { Template } from '../api/types'

const msg = useMessage()
const dialog = useDialog()
const list = ref<Template[]>([])
const cur = ref<Template | null>(null)
const body = ref('')
const saving = ref(false)
const preview = ref('')

async function load() {
  list.value = await api.templates()
  if (!cur.value && list.value.length) select(list.value[0])
}
function select(t: Template) { cur.value = t; body.value = t.body; preview.value = '' }
async function save() {
  if (!cur.value) return
  saving.value = true
  try { await api.saveTemplate(cur.value.name, body.value); msg.success('已保存'); await load() } catch (e: any) { msg.error(e.message) } finally { saving.value = false }
}
function create() {
  const name = ref('')
  dialog.create({
    title: '新建模板', positiveText: '创建', negativeText: '取消',
    content: () => h(NInput, { value: name.value, 'onUpdate:value': (v: string) => (name.value = v), placeholder: '模板名（字母数字-_）', autofocus: true }),
    onPositiveClick: async () => {
      if (!/^[A-Za-z0-9_-]+$/.test(name.value)) { msg.error('名称只能包含字母、数字、- 和 _'); return false }
      try {
        await api.saveTemplate(name.value, body.value || list.value[0]?.body || '')
        await load(); select(list.value.find((t) => t.name === name.value)!)
      } catch (e: any) { msg.error(e.message); return false }
    },
  })
}
async function remove(t: Template) {
  try { await api.deleteTemplate(t.name); if (cur.value?.name === t.name) cur.value = null; await load() } catch (e: any) { msg.error(e.message) }
}
async function doPreview() {
  if (!cur.value) return
  try {
    await api.saveTemplate(cur.value.name, body.value)
    preview.value = (await api.render(cur.value.name, {
      Title: 'Example.Movie.2024.1080p.BluRay.x264-GRP',
      MediaInfo: 'General\nComplete name : Example.mkv\nFormat : Matroska\n…',
      BDInfo: 'Disc Title: Example\nDisc Size: 45,000,000,000 bytes\n…',
      Images: ['https://img.example/1.png', 'https://img.example/2.png'],
      Torrent: 'https://box/api/output/Example/Example.torrent',
      Source: '/data/Example.Movie.2024',
    })).text
  } catch (e: any) { msg.error(e.message) }
}
onMounted(load)
</script>

<template>
  <div>
    <div class="page-head"><h2>BBCode 模板</h2><NButton size="small" type="primary" @click="create">新建模板</NButton></div>
    <NGrid :cols="12" :x-gap="16" :y-gap="16" item-responsive responsive="screen">
      <NGi span="12 m:3">
        <NCard size="small" title="模板列表">
          <NEmpty v-if="!list.length" description="无模板" />
          <NList v-else hoverable clickable>
            <NListItem v-for="t in list" :key="t.name" @click="select(t)" :class="{ sel: cur?.name === t.name }">
              <NSpace justify="space-between" align="center">
                <span>{{ t.name }} <NTag v-if="t.name === 'default'" size="tiny" :bordered="false">内置</NTag></span>
                <NPopconfirm v-if="t.name !== 'default'" @positive-click.stop="remove(t)" positive-text="删除" negative-text="取消">
                  <template #trigger><NButton size="tiny" quaternary type="error" @click.stop>删</NButton></template>删除模板 {{ t.name }}？
                </NPopconfirm>
              </NSpace>
            </NListItem>
          </NList>
        </NCard>
      </NGi>
      <NGi span="12 m:9">
        <NCard v-if="cur" size="small" :title="'编辑：' + cur.name">
          <NAlert type="info" :bordered="false" style="margin-bottom: 10px">
            Go text/template 语法。可用字段：<code>.Title .MediaInfo .BDInfo .BDInfoFull .Images .Thumbs .Torrent .Source .Extra</code>。
            例：<code v-pre>{{range .Images}}[img]{{.}}[/img]{{end}}</code>
          </NAlert>
          <NInput v-model:value="body" type="textarea" :autosize="{ minRows: 12, maxRows: 30 }" class="mono" />
          <NSpace justify="end" style="margin-top: 10px">
            <NButton @click="doPreview">保存并预览</NButton>
            <NButton type="primary" :loading="saving" @click="save">保存</NButton>
          </NSpace>
          <template v-if="preview">
            <div style="margin: 12px 0 6px; font-weight: 500">预览（示例数据）</div>
            <pre class="report mono">{{ preview }}</pre>
          </template>
        </NCard>
      </NGi>
    </NGrid>
  </div>
</template>

<style scoped>
.sel { background: rgba(91,141,239,.12); border-radius: 4px; }
</style>
