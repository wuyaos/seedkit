import { createRouter, createWebHistory } from 'vue-router'
import { session } from './stores/session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('./views/Login.vue'), meta: { public: true } },
    { path: '/', redirect: '/files' },
    { path: '/files', component: () => import('./views/Files.vue') },
    { path: '/tasks', component: () => import('./views/Tasks.vue') },
    { path: '/publish', component: () => import('./views/Publish.vue') },
    { path: '/templates', component: () => import('./views/Templates.vue') },
    { path: '/settings', component: () => import('./views/Settings.vue') },
  ],
})

router.beforeEach(async (to) => {
  if (!session.loaded) await session.refresh()
  if (to.meta.public) return true
  if (!session.authed) return { path: '/login', query: { next: to.fullPath } }
  return true
})
