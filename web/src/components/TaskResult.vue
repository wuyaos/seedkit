<script setup lang="ts">
import { computed } from 'vue'
import { NImage, NImageGroup, NSpace, NButton, NTag, NDescriptions, NDescriptionsItem, NAlert, NTabs, NTabPane, useMessage } from 'naive-ui'
import type { Task, ScreenshotResult, TorrentResult, BDInfoResult, PipelineResult, UploadResult } from '../api/types'
import { bytes, duration, copyText } from '../utils/format'

const props = defineProps<{ task: Task }>()
const msg = useMessage()
const r = computed<any>(() => props.task.result)
const kind = computed(() => props.task.kind)
const shot = computed<ScreenshotResult | undefined>(() => kind.value === 'screenshot' ? r.value : kind.value === 'pipeline' ? (r.value as PipelineResult)?.screenshot : undefined)
const tor = computed<TorrentResult | undefined>(() => kind.value === 'torrent' ? r.value : kind.value === 'pipeline' ? (r.value as PipelineResult)?.torrent : undefined)
const bd = computed<BDInfoResult | undefined>(() => kind.value === 'bdinfo' ? r.value : kind.value === 'pipeline' ? (r.value as PipelineResult)?.bdinfo : undefined)
const mi = computed<string | undefined>(() => kind.value === 'mediainfo' ? r.value?.text : kind.value === 'pipeline' ? (r.value as PipelineResult)?.mediainfo : undefined)
const ups = computed<UploadResult[] | undefined>(() => kind.value === 'upload' ? r.value : undefined)
const urls = computed(() => (shot.value?.shots ?? []).map((s) => s.url).filter(Boolean))

async function copy(t: string, what = '') { if (await copyText(t)) msg.success('已复制' + what) }
const imgTags = () => urls.value.map((u) => '[img]' + u + '[/img]').join('\n')
</script>

<template>
  <div>
    <template v-if="mi">
      <NSpace justify="end" style="margin-bottom:6px"><NButton size="tiny" @click="copy(mi!, ' MediaInfo')">复制</NButton></NSpace>
      <pre class="report mono">{{ mi }}</pre>
    </template>

    <template v-if="bd">
      <NAlert v-if="bd.errors && Object.keys(bd.errors).length" type="warning" :bordered="false" style="margin-bottom:8px">
        扫描有 {{ Object.keys(bd.errors).length }} 个文件出错，报告可能不完整
      </NAlert>
      <NTabs type="segment" size="small" animated>
        <NTabPane name="quick" tab="Quick Summary">
          <NSpace justify="end" style="margin-bottom:6px"><NButton size="tiny" @click="copy(bd!.quickSummary, ' Quick Summary')">复制</NButton></NSpace>
          <pre class="report mono">{{ bd.quickSummary }}</pre>
        </NTabPane>
        <NTabPane name="forums" tab="论坛格式">
          <NSpace justify="end" style="margin-bottom:6px"><NButton size="tiny" @click="copy(bd!.forums)">复制</NButton></NSpace>
          <pre class="report mono">{{ bd.forums }}</pre>
        </NTabPane>
        <NTabPane name="full" tab="完整报告">
          <NSpace justify="end" style="margin-bottom:6px"><NButton size="tiny" @click="copy(bd!.report)">复制</NButton></NSpace>
          <pre class="report mono">{{ bd.report }}</pre>
        </NTabPane>
      </NTabs>
    </template>

    <template v-if="shot">
      <NSpace size="small" style="margin-bottom:8px" align="center">
        <NTag size="small" :bordered="false">时长 {{ duration(shot.duration) }}</NTag>
        <NTag v-if="shot.hdr" size="small" type="warning" :bordered="false">HDR → SDR</NTag>
        <NTag v-if="shot.subtitle" size="small" :bordered="false">字幕 {{ shot.subtitle }}</NTag>
        <NTag size="small" :bordered="false">{{ shot.shots.length }} 张</NTag>
        <NButton v-if="urls.length" size="tiny" type="primary" @click="copy(imgTags(), ' [img] 标签')">复制 [img] 标签</NButton>
        <NButton v-if="urls.length" size="tiny" @click="copy(urls.join('\n'), ' 直链')">复制直链</NButton>
      </NSpace>
      <NAlert v-for="w in shot.warnings" :key="w" type="warning" :bordered="false" style="margin-bottom:6px">{{ w }}</NAlert>
      <NImageGroup>
        <NSpace size="small">
          <div v-for="s in shot.shots" :key="s.index" class="thumb">
            <NImage :src="s.web" width="160" object-fit="cover" lazy :alt="'#' + s.index" />
            <div class="cap">
              <span>#{{ s.index }} · {{ duration(s.seconds) }} · {{ bytes(s.bytes) }}</span>
              <a v-if="s.url" :href="s.url" target="_blank" rel="noopener">已上传</a>
            </div>
          </div>
        </NSpace>
      </NImageGroup>
    </template>

    <template v-if="tor">
      <NDescriptions label-placement="left" size="small" :column="2" bordered>
        <NDescriptionsItem label="InfoHash"><span class="mono">{{ tor.torrent.infoHash }}</span></NDescriptionsItem>
        <NDescriptionsItem label="大小">{{ bytes(tor.torrent.totalBytes) }}</NDescriptionsItem>
        <NDescriptionsItem label="分块">{{ tor.torrent.pieces }} × {{ bytes(tor.torrent.pieceSize) }}</NDescriptionsItem>
        <NDescriptionsItem label="文件数">{{ tor.torrent.files }}</NDescriptionsItem>
      </NDescriptions>
      <NSpace style="margin-top:8px">
        <NButton size="small" type="primary" tag="a" :href="tor.web" download>下载 .torrent</NButton>
      </NSpace>
    </template>

    <template v-if="ups">
      <div v-for="u in ups" :key="u.file" class="up-row">
        <NTag size="tiny" :type="u.error ? 'error' : 'success'" :bordered="false">{{ u.host }}</NTag>
        <span class="mono">{{ u.file.split(/[\\/]/).pop() }}</span>
        <a v-if="u.url" :href="u.url" target="_blank" rel="noopener" class="mono">{{ u.url }}</a>
        <span v-else class="err">{{ u.error }}</span>
      </div>
      <NButton v-if="ups.some((u) => u.url)" size="tiny" style="margin-top:6px" @click="copy(ups!.filter((u) => u.url).map((u) => '[img]' + u.url + '[/img]').join('\n'))">复制 [img] 标签</NButton>
    </template>

    <pre v-if="!mi && !bd && !shot && !tor && !ups && r" class="report mono">{{ JSON.stringify(r, null, 2) }}</pre>
  </div>
</template>

<style scoped>
.thumb { width: 160px; }
.cap { font-size: 11px; opacity: .7; display: flex; justify-content: space-between; margin-top: 2px; }
.up-row { display: flex; gap: 8px; align-items: center; font-size: 12px; margin: 3px 0; flex-wrap: wrap; }
.err { color: #e06c6c; }
</style>
