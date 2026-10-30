import type * as T from './types'

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

async function req<R>(method: string, url: string, body?: unknown): Promise<R> {
  const res = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  })
  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent('seedkit:unauthorized'))
  }
  const text = await res.text()
  let data: any = null
  try { data = text ? JSON.parse(text) : null } catch { data = { error: text } }
  if (!res.ok) throw new ApiError(res.status, data?.error ?? res.statusText)
  return data as R
}
const get = <R>(url: string) => req<R>('GET', url)
const post = <R>(url: string, body?: unknown) => req<R>('POST', url, body ?? {})
const put = <R>(url: string, body?: unknown) => req<R>('PUT', url, body ?? {})
const del = <R>(url: string) => req<R>('DELETE', url)
const q = (o: Record<string, string>) => '?' + new URLSearchParams(o).toString()

export const api = {
  status: () => get<T.Status>('/api/status'),
  setup: (Username: string, Password: string) => post('/api/auth/setup', { Username, Password }),
  login: (Username: string, Password: string) => post('/api/auth/login', { Username, Password }),
  logout: () => post('/api/auth/logout'),

  roots: () => get<T.Root[]>('/api/fs/roots'),
  list: (path: string) => get<T.Entry[]>('/api/fs/list' + q({ path })),
  stat: (path: string) => get<T.Entry>('/api/fs/stat' + q({ path })),
  text: (path: string) => get<{ text: string; truncated: boolean }>('/api/fs/text' + q({ path })),
  downloadURL: (path: string) => '/api/fs/download' + q({ path }),
  mkdir: (Path: string) => post('/api/fs/mkdir', { Path }),
  rename: (Path: string, Name: string) => post('/api/fs/rename', { Path, Name }),
  remove: (Paths: string[]) => post('/api/fs/delete', { Paths }),
  move: (Paths: string[], Dest: string) => post<T.Task>('/api/fs/move', { Paths, Dest }),
  copy: (Paths: string[], Dest: string) => post<T.Task>('/api/fs/copy', { Paths, Dest }),

  tasks: () => get<T.Task[]>('/api/tasks'),
  task: (id: string) => get<T.Task>('/api/tasks/' + id),
  cancel: (id: string) => post('/api/tasks/' + id + '/cancel'),
  deleteTask: (id: string) => del('/api/tasks/' + id),

  mediainfo: (Path: string) => post<T.Task>('/api/media/info', { Path }),
  playlists: (path: string) => get<{ disc: T.BDInfoResult['disc']; playlists: T.Playlist[] }>('/api/media/playlists' + q({ path })),
  bdinfo: (Path: string, Playlist = '', SummaryOnly = false) => post<T.Task>('/api/media/bdinfo', { Path, Playlist, SummaryOnly }),
  screenshot: (Path: string, o: T.ScreenshotReq) => post<T.Task>('/api/screenshot', { Path, ...o }),
  upload: (Files: string[], Host = '') => post<T.Task>('/api/upload', { Files, Host }),
  torrent: (Path: string, o: T.TorrentReq) => post<T.Task>('/api/torrent', { Path, ...o }),
  pipeline: (body: { Path: string; Info: boolean; Playlist?: string; Screenshot?: T.ScreenshotReq | null; Torrent?: T.TorrentReq | null }) =>
    post<T.Task>('/api/pipeline', body),

  templates: () => get<T.Template[]>('/api/templates'),
  saveTemplate: (name: string, Body: string) => put('/api/templates/' + encodeURIComponent(name), { Body }),
  deleteTemplate: (name: string) => del('/api/templates/' + encodeURIComponent(name)),
  render: (Template: string, Data: T.BBData) => post<{ text: string }>('/api/bbcode/render', { Template, Data }),

  settings: () => get<T.Config>('/api/settings'),
  saveSettings: (c: T.Config) => put<{ ok: boolean; tools: T.ToolSet; restartRequired: boolean }>('/api/settings', c),
}
