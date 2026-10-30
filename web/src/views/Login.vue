<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NCard, NForm, NFormItem, NInput, NButton, NAlert, NSpace } from 'naive-ui'
import { api } from '../api/client'
import { session } from '../stores/session'

const router = useRouter()
const route = useRoute()
const username = ref('')
const password = ref('')
const password2 = ref('')
const err = ref('')
const busy = ref(false)

async function submit() {
  err.value = ''
  if (session.needsSetup && password.value !== password2.value) { err.value = '两次输入的密码不一致'; return }
  busy.value = true
  try {
    if (session.needsSetup) await api.setup(username.value, password.value)
    else await api.login(username.value, password.value)
    await session.refresh()
    router.replace((route.query.next as string) || '/files')
  } catch (e: any) {
    err.value = e.message || '登录失败'
  } finally { busy.value = false }
}
</script>

<template>
  <div class="login-wrap">
    <NCard class="login-card" :title="session.needsSetup ? '初始化账户' : '登录 seedkit'" size="large">
      <NAlert v-if="session.needsSetup" type="info" style="margin-bottom: 14px" :bordered="false">
        首次使用，请设置管理员账户。密码至少 6 位。
      </NAlert>
      <NForm @submit.prevent="submit">
        <NFormItem label="用户名"><NInput v-model:value="username" autofocus placeholder="admin" /></NFormItem>
        <NFormItem label="密码"><NInput v-model:value="password" type="password" show-password-on="click" @keyup.enter="submit" /></NFormItem>
        <NFormItem v-if="session.needsSetup" label="确认密码">
          <NInput v-model:value="password2" type="password" show-password-on="click" @keyup.enter="submit" />
        </NFormItem>
        <NAlert v-if="err" type="error" :bordered="false" style="margin-bottom: 12px">{{ err }}</NAlert>
        <NSpace justify="end">
          <NButton type="primary" :loading="busy" attr-type="submit" @click="submit">
            {{ session.needsSetup ? '创建并登录' : '登录' }}
          </NButton>
        </NSpace>
      </NForm>
    </NCard>
  </div>
</template>

<style scoped>
.login-wrap { min-height: 100vh; display: grid; place-items: center; }
.login-card { width: 380px; max-width: 92vw; }
</style>
