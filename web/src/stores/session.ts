import { reactive } from 'vue'
import { api } from '../api/client'
import type { Status } from '../api/types'

export const session = reactive({
  loaded: false,
  authed: false,
  needsSetup: false,
  version: '',
  tools: null as Status['tools'] | null,
  zscale: false,
  async refresh() {
    try {
      const s = await api.status()
      this.authed = s.authed
      this.needsSetup = s.needsSetup
      this.version = s.version
      this.tools = s.tools
      this.zscale = s.zscale
    } catch {
      this.authed = false
    }
    this.loaded = true
  },
})

window.addEventListener('seedkit:unauthorized', () => { session.authed = false })
