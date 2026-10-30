import { reactive, computed } from 'vue'
import { api } from '../api/client'
import type { Task, TaskEvent } from '../api/types'

const state = reactive({
  list: [] as Task[],
  connected: false,
})
let es: EventSource | null = null
let retry = 0

function upsert(t: Task) {
  const i = state.list.findIndex((x) => x.id === t.id)
  if (i >= 0) state.list[i] = t
  else state.list.unshift(t)
}

export const tasks = {
  state,
  active: computed(() => state.list.filter((t) => t.status === 'queued' || t.status === 'running')),
  async load() {
    state.list = (await api.tasks()).sort((a, b) => (a.created < b.created ? 1 : -1))
  },
  connect() {
    if (es) return
    es = new EventSource('/api/tasks/events')
    es.addEventListener('hello', () => { state.connected = true; retry = 0; this.load() })
    es.addEventListener('task', (e) => upsert((JSON.parse((e as MessageEvent).data) as TaskEvent).task))
    es.onerror = () => {
      state.connected = false
      es?.close(); es = null
      const wait = Math.min(30000, 1000 * 2 ** retry++)
      setTimeout(() => this.connect(), wait)
    }
  },
  disconnect() { es?.close(); es = null; state.connected = false },
  get(id: string) { return state.list.find((t) => t.id === id) },
  /** Resolve when the task reaches a terminal state. */
  wait(id: string): Promise<Task> {
    return new Promise((resolve) => {
      const check = () => {
        const t = this.get(id)
        if (t && (t.status === 'done' || t.status === 'failed' || t.status === 'cancelled')) { resolve(t); return true }
        return false
      }
      if (check()) return
      const iv = setInterval(() => { if (check()) clearInterval(iv) }, 500)
    })
  },
  async cancel(id: string) { await api.cancel(id) },
  async remove(id: string) { await api.deleteTask(id); state.list = state.list.filter((t) => t.id !== id) },
}
