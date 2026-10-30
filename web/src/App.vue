<script setup lang="ts">
import { computed, h, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import {
  NConfigProvider, NMessageProvider, NDialogProvider, NNotificationProvider, NLayout, NLayoutSider,
  NLayoutContent, NMenu, NSpace, NButton, NBadge, NIcon, NTag, NTooltip, NGlobalStyle, darkTheme, zhCN, dateZhCN,
  type MenuOption, type GlobalThemeOverrides,
} from 'naive-ui'
import {
  FolderOpenOutline, ListOutline, RocketOutline, DocumentTextOutline, SettingsOutline,
  LogOutOutline, SunnyOutline, MoonOutline,
} from '@vicons/ionicons5'
import { session } from './stores/session'
import { tasks } from './stores/tasks'
import { api } from './api/client'

const route = useRoute()
const router = useRouter()
const dark = ref(localStorage.getItem('seedkit.theme') !== 'light')
watch(dark, (v) => localStorage.setItem('seedkit.theme', v ? 'dark' : 'light'))
const collapsed = ref(window.innerWidth < 900)

const overrides: GlobalThemeOverrides = {
  common: { primaryColor: '#5B8DEF', primaryColorHover: '#7AA3F5', primaryColorPressed: '#3F6FD1', borderRadius: '6px' },
}

const icon = (c: any) => () => h(NIcon, null, { default: () => h(c) })
const link = (to: string, label: string) => () => h(RouterLink, { to }, { default: () => label })
const menu = computed<MenuOption[]>(() => [
  { key: '/files', label: link('/files', '文件'), icon: icon(FolderOpenOutline) },
  {
    key: '/tasks', icon: icon(ListOutline),
    label: () => h(NSpace, { align: 'center', size: 6 }, { default: () => [
      h(RouterLink, { to: '/tasks' }, { default: () => '任务' }),
      tasks.active.value.length ? h(NBadge, { value: tasks.active.value.length, processing: true }) : null,
    ] }),
  },
  { key: '/publish', label: link('/publish', '发布'), icon: icon(RocketOutline) },
  { key: '/templates', label: link('/templates', '模板'), icon: icon(DocumentTextOutline) },
  { key: '/settings', label: link('/settings', '设置'), icon: icon(SettingsOutline) },
])

onMounted(() => { if (session.authed) tasks.connect() })
watch(() => session.authed, (v) => (v ? tasks.connect() : tasks.disconnect()))

async function logout() {
  await api.logout()
  session.authed = false
  router.push('/login')
}
</script>

<template>
  <NConfigProvider :theme="dark ? darkTheme : null" :theme-overrides="overrides" :locale="zhCN" :date-locale="dateZhCN">
    <NGlobalStyle />
    <NNotificationProvider placement="bottom-right">
      <NDialogProvider>
        <NMessageProvider>
          <NLayout v-if="session.authed && !route.meta.public" has-sider style="height: 100vh">
            <NLayoutSider bordered collapse-mode="width" :collapsed-width="56" :width="200" :collapsed="collapsed"
              show-trigger @collapse="collapsed = true" @expand="collapsed = false">
              <div class="brand">
                <span class="logo">⛵</span>
                <span v-if="!collapsed" class="name">seedkit</span>
              </div>
              <NMenu :value="route.path" :options="menu" :collapsed="collapsed" :collapsed-width="56" :collapsed-icon-size="20" />
              <div class="sider-foot">
                <NTooltip><template #trigger>
                  <NButton quaternary circle size="small" @click="dark = !dark"><NIcon><MoonOutline v-if="dark" /><SunnyOutline v-else /></NIcon></NButton>
                </template>切换主题</NTooltip>
                <NTooltip><template #trigger>
                  <NButton quaternary circle size="small" @click="logout"><NIcon><LogOutOutline /></NIcon></NButton>
                </template>退出登录</NTooltip>
                <NTag v-if="!collapsed" size="tiny" :bordered="false">{{ session.version }}</NTag>
              </div>
            </NLayoutSider>
            <NLayoutContent content-style="padding: 20px; min-height: 100vh" :native-scrollbar="false">
              <RouterView />
            </NLayoutContent>
          </NLayout>
          <NLayout v-else style="min-height: 100vh"><RouterView /></NLayout>
        </NMessageProvider>
      </NDialogProvider>
    </NNotificationProvider>
  </NConfigProvider>
</template>

<style>
html, body { margin: 0; font-family: -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", Roboto, sans-serif; }
#app { min-height: 100vh; }
.mono { font-family: ui-monospace, "Cascadia Mono", Consolas, Menlo, monospace; font-size: 12px; }
.brand { display: flex; align-items: center; gap: 8px; padding: 16px 16px 8px; font-weight: 600; font-size: 16px; white-space: nowrap; overflow: hidden; }
.brand .logo { font-size: 20px; }
.sider-foot { position: absolute; bottom: 12px; left: 0; right: 0; display: flex; gap: 6px; align-items: center; justify-content: center; flex-wrap: wrap; }
.page-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; flex-wrap: wrap; }
.page-head h2 { margin: 0; font-size: 18px; font-weight: 600; }
.n-layout-sider { position: relative; }
pre.report { white-space: pre; overflow: auto; max-height: 60vh; padding: 10px; border-radius: 6px; background: rgba(127,127,127,.08); margin: 0; }
</style>
