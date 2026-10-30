<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  NCard, NForm, NFormItem, NInput, NInputNumber, NSwitch, NSelect, NButton, NSpace, NTabs, NTabPane, NDynamicInput,
  NAlert, NTag, NDescriptions, NDescriptionsItem, NSpin, useMessage,
} from 'naive-ui'
import { api } from '../api/client'
import type { Config } from '../api/types'
import { session } from '../stores/session'

const msg = useMessage()
const cfg = ref<Config | null>(null)
const roots = ref<{ key: string; value: string }[]>([])
const saving = ref(false)
const restart = ref(false)

async function load() {
  cfg.value = await api.settings()
  roots.value = Object.entries(cfg.value.paths.roots || {}).map(([key, value]) => ({ key, value }))
}
async function save() {
  if (!cfg.value) return
  const r: Record<string, string> = {}
  for (const x of roots.value) if (x.key && x.value) r[x.key] = x.value
  cfg.value.paths.roots = r
  saving.value = true
  try {
    const res = await api.saveSettings(cfg.value)
    restart.value = res.restartRequired
    msg.success('已保存')
    await session.refresh()
    await load()
  } catch (e: any) { msg.error(e.message) } finally { saving.value = false }
}
onMounted(load)
const tonemaps = [{ label: 'hable（默认，电影感）', value: 'hable' }, { label: 'mobius（保色彩）', value: 'mobius' }, { label: 'reinhard（偏亮）', value: 'reinhard' }, { label: 'bt2390（标准）', value: 'bt2390' }, { label: '不做色调映射', value: 'none' }]
const hosts = [{ label: 'picgo.net', value: 'picgo' }, { label: 'pixhost', value: 'pixhost' }]
const pieceSizes = [{ label: '自动（按大小）', value: 0 }, ...[16, 18, 20, 21, 22, 23, 24, 25, 26].map((e) => ({ label: (2 ** e / 1048576 >= 1 ? 2 ** e / 1048576 + ' MiB' : 2 ** e / 1024 + ' KiB') + ' (2^' + e + ')', value: e }))]
</script>

<template>
  <div>
    <div class="page-head"><h2>设置</h2>
      <NButton type="primary" :loading="saving" :disabled="!cfg" @click="save">保存</NButton>
    </div>
    <NAlert v-if="restart" type="warning" :bordered="false" style="margin-bottom: 12px">监听地址已修改，需要重启 seedkit 才会生效。</NAlert>
    <NSpin :show="!cfg">
      <NTabs v-if="cfg" type="line" animated>
        <NTabPane name="paths" tab="目录">
          <NCard size="small">
            <NForm label-placement="top">
              <NFormItem label="根目录（文件管理器只能访问这些目录）">
                <NDynamicInput v-model:value="roots" :on-create="() => ({ key: '', value: '' })">
                  <template #default="{ value }">
                    <NSpace :wrap="false" style="width: 100%">
                      <NInput v-model:value="value.key" placeholder="显示名，如 data" style="width: 160px" />
                      <NInput v-model:value="value.value" placeholder="绝对路径，如 /home/user/downloads" style="width: 420px" />
                    </NSpace>
                  </template>
                </NDynamicInput>
              </NFormItem>
              <NFormItem label="输出目录（截图 / 种子 / 报告）"><NInput v-model:value="cfg.paths.output" /></NFormItem>
            </NForm>
          </NCard>
        </NTabPane>

        <NTabPane name="screenshot" tab="截图">
          <NCard size="small">
            <NForm label-placement="left" label-width="140">
              <NFormItem label="默认张数"><NInputNumber v-model:value="cfg.screenshot.count" :min="1" :max="20" /></NFormItem>
              <NFormItem label="首尾跳过 %"><NInputNumber v-model:value="cfg.screenshot.skip_percent" :min="0" :max="40" /></NFormItem>
              <NFormItem label="默认烧录字幕"><NSwitch v-model:value="cfg.screenshot.subtitles" /></NFormItem>
              <NFormItem label="PNG 量化压缩"><NSwitch v-model:value="cfg.screenshot.compress" /><span style="margin-left:10px;opacity:.7;font-size:12px">256 色 + Floyd-Steinberg，约缩小 60-70%</span></NFormItem>
              <NFormItem label="HDR 色调映射"><NSelect v-model:value="cfg.screenshot.tonemap" :options="tonemaps" style="width: 220px" />
                <NTag v-if="!session.zscale" type="warning" size="small" :bordered="false" style="margin-left:10px">当前 ffmpeg 无 zscale，HDR 将用 colorspace 滤镜回退</NTag>
              </NFormItem>
            </NForm>
          </NCard>
        </NTabPane>

        <NTabPane name="upload" tab="图床">
          <NCard size="small">
            <NForm label-placement="left" label-width="140">
              <NFormItem label="默认图床"><NSelect v-model:value="cfg.upload.default" :options="hosts" style="width: 160px" /></NFormItem>
              <NFormItem label="picgo.net Endpoint"><NInput v-model:value="cfg.upload.picgo.endpoint" /></NFormItem>
              <NFormItem label="picgo.net API Key"><NInput v-model:value="cfg.upload.picgo.api_key" type="password" show-password-on="click" placeholder="留空保持不变" /></NFormItem>
              <NFormItem label="picgo.net 相册 ID"><NInput v-model:value="cfg.upload.picgo.album" placeholder="可选" /></NFormItem>
              <NFormItem label="pixhost 内容分级"><NSelect v-model:value="cfg.upload.pixhost.content_type" :options="[{ label: '安全', value: 0 }, { label: '成人', value: 1 }]" style="width: 120px" /></NFormItem>
              <NFormItem label="pixhost 缩略图宽"><NInputNumber v-model:value="cfg.upload.pixhost.max_th" :min="150" :max="500" /></NFormItem>
            </NForm>
          </NCard>
        </NTabPane>

        <NTabPane name="torrent" tab="制种">
          <NCard size="small">
            <NForm label-placement="left" label-width="140">
              <NFormItem label="默认 Tracker"><NInput v-model:value="cfg.torrent.announce" placeholder="https://tracker/announce?passkey=…" /></NFormItem>
              <NFormItem label="Source 标记"><NInput v-model:value="cfg.torrent.source" placeholder="站点简称，使 infohash 唯一" /></NFormItem>
              <NFormItem label="Private"><NSwitch v-model:value="cfg.torrent.private" /></NFormItem>
              <NFormItem label="分块大小"><NSelect v-model:value="cfg.torrent.piece_size" :options="pieceSizes" style="width: 200px" /></NFormItem>
              <NFormItem label="Comment"><NInput v-model:value="cfg.torrent.comment" /></NFormItem>
            </NForm>
          </NCard>
        </NTabPane>

        <NTabPane name="system" tab="系统">
          <NCard size="small" title="外部工具">
            <NDescriptions label-placement="left" size="small" :column="1" bordered>
              <NDescriptionsItem v-for="t in [session.tools?.ffmpeg, session.tools?.ffprobe]" :key="t?.name" :label="t?.name">
                <template v-if="t?.found">
                  <NTag size="small" type="success" :bordered="false">{{ t.version || '已找到' }}</NTag>
                  <span class="mono" style="margin-left: 8px">{{ t.path }}</span>
                  <NTag size="tiny" :bordered="false" style="margin-left: 8px">{{ t.source }}</NTag>
                </template>
                <NTag v-else size="small" type="error" :bordered="false">未找到</NTag>
              </NDescriptionsItem>
            </NDescriptions>
            <NForm label-placement="left" label-width="140" style="margin-top: 12px">
              <NFormItem label="ffmpeg 路径"><NInput v-model:value="cfg.tools.ffmpeg" placeholder="留空自动探测 PATH / 内嵌" /></NFormItem>
              <NFormItem label="ffprobe 路径"><NInput v-model:value="cfg.tools.ffprobe" placeholder="留空自动探测" /></NFormItem>
            </NForm>
          </NCard>
          <NCard size="small" title="服务" style="margin-top: 12px">
            <NForm label-placement="left" label-width="140">
              <NFormItem label="监听地址"><NInput v-model:value="cfg.server.listen" placeholder=":18080" style="width: 200px" /></NFormItem>
              <NFormItem label="重任务并发"><NInputNumber v-model:value="cfg.tasks.heavy_concurrency" :min="1" :max="4" /><span style="margin-left:10px;opacity:.7;font-size:12px">BDInfo / 截图 / 制种同时运行数，2 GB 内存建议 1</span></NFormItem>
              <NFormItem label="任务历史保留"><NInputNumber v-model:value="cfg.tasks.history_keep" :min="10" :max="1000" /></NFormItem>
              <NFormItem label="会话有效期（天）"><NInputNumber v-model:value="cfg.auth.session_days" :min="1" :max="365" /></NFormItem>
            </NForm>
          </NCard>
        </NTabPane>
      </NTabs>
    </NSpin>
  </div>
</template>
