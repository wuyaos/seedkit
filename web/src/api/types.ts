export interface Status {
  version: string
  needsSetup: boolean
  authed: boolean
  tools: ToolSet
  zscale: boolean
}
export interface Tool { name: string; path: string; version: string; found: boolean; source: string }
export interface ToolSet { ffmpeg: Tool; ffprobe: Tool }

export interface Root { name: string; path: string }
export type Kind = 'video' | 'image' | 'text' | 'archive' | 'torrent' | 'bdmv' | 'iso' | 'other' | 'dir'
export interface Entry {
  name: string
  path: string
  isDir: boolean
  size: number
  modTime: string
  kind: Kind
}

export type TaskStatus = 'queued' | 'running' | 'done' | 'failed' | 'cancelled'
export interface Task {
  id: string
  kind: string
  title: string
  status: TaskStatus
  progress: number
  message: string
  error?: string
  heavy: boolean
  created: string
  started?: string
  finished?: string
  result?: unknown
  log?: string[]
}
export interface TaskEvent { type: string; task: Task }

export interface Shot {
  index: number
  seconds: number
  path: string
  bytes: number
  rawBytes: number
  url: string
  web: string
}
export interface UploadResult { file: string; url: string; host: string; error?: string }
export interface ScreenshotResult {
  source: string
  duration: number
  hdr: boolean
  subtitle: string
  shots: Shot[]
  warnings?: string[]
  uploads?: UploadResult[]
}
export interface TorrentResult {
  torrent: { path: string; infoHash: string; totalBytes: number; pieces: number; pieceSize: number; files: number }
  web: string
}
export interface BDInfoResult {
  report: string
  quickSummary: string
  forums: string
  disc: { Path: string; Title: string; Label: string; SizeBytes: number }
  playlists: Playlist[]
  errors?: Record<string, string>
}
export interface Playlist {
  Name: string
  LengthSeconds: number
  SizeBytes: number
  TotalBitrateBps: number
  HasHiddenTracks: boolean
  IsValid: boolean
}
export interface PipelineResult {
  mediainfo?: string
  bdinfo?: BDInfoResult
  screenshot?: ScreenshotResult
  torrent?: TorrentResult
}

export interface ScreenshotReq { Count?: number; Language?: string; Upload?: boolean; Host?: string; Compress?: boolean | null }
export interface TorrentReq { Announce?: string; Source?: string; Private?: boolean | null; Comment?: string; PieceSize?: number }

export interface Template { name: string; body: string }
export interface BBData {
  Title?: string
  MediaInfo?: string
  BDInfo?: string
  BDInfoFull?: string
  Images?: string[]
  Thumbs?: string[]
  Torrent?: string
  Source?: string
  Extra?: Record<string, string>
}

export interface Config {
  server: { listen: string }
  auth: { username: string; password_hash: string; session_days: number }
  paths: { roots: Record<string, string>; output: string }
  tasks: { heavy_concurrency: number; history_keep: number }
  tools: { ffmpeg: string; ffprobe: string }
  screenshot: { count: number; subtitles: boolean; compress: boolean; skip_percent: number; tonemap: string }
  upload: {
    default: string
    picgo: { endpoint: string; api_key: string; album?: string }
    pixhost: { content_type: number; max_th: number }
    extra?: Record<string, string>
  }
  torrent: { announce: string; private: boolean; source: string; piece_size: number; comment: string }
}
