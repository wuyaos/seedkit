# Agent Note: 单一静态二进制架构与依赖面

Status: implemented

## Problem

目标是给 PT 盒子（1-2 GB 内存，常无 root）做一个发种辅助工具：文件管理、MediaInfo/BDInfo、截图+图床、制种+BBCode。
两条硬约束：常驻占用低；**非 Docker 时也要无依赖安装**（下载一个文件就能跑）。

参考项目都做不到：旧 bdweb 镜像是 Java + bash 脚本，常驻 300 MB；BDInfoWebUI 是 Node + .NET 运行时；minfo 虽是 Go 但 exec 外部 BDInfo/mkbrr/pngquant。

## Decision

主程序是 **CGO_ENABLED=0 的 Go 单二进制**，前端 Alpine.js + 原生 CSS 的静态资源由 `web/embed.go` 直接嵌入，无 npm 构建和 Node 常驻进程。Go 依赖带来 GPL 许可约束；正式发行前仍需核对依赖及视觉资源的再分发要求。

依赖面只剩 **ffmpeg + ffprobe**，其余全部进程内完成：

| 能力 | 实现 | 许可 |
|---|---|---|
| BDInfo | `github.com/autobrr/go-bdinfo/pkg/bdinfo` 作为库 import，`bdinfo.Run(ctx, Options{OnProgress})` 拿进度与报告 | GPL-2.0-or-later；固定到 c4a4d2f7dd39 明确许可提交 |
| MediaInfo | `github.com/autobrr/go-mediainfo` 库，`AnalyzeFile` + `Render(OutputText)` | GPL-2.0-or-later |
| PNG 压缩（替代 pngquant） | `github.com/ericpauley/go-quantize` Median Cut 生成 256 色调色板 + 标准库 `draw.FloydSteinberg` 抖动 + `image/png` Paletted 编码 | MIT |
| 图片唯一化（替代 ImageMagick 写注释） | Go 写 PNG `tEXt` 块 | 标准库 |
| 制种 | `github.com/autobrr/mkbrr/torrent` 作为库（`CreateTorrent` + 自写文件），封装加 ctx 取消与进度转发 | GPL-2.0-or-later |
| 截图 | exec `ffmpeg`/`ffprobe` | 外部 |

ffmpeg 解析顺序：配置文件显式路径 → `-tags bundleffmpeg` 构建时内嵌并释放到数据目录的静态二进制 → `PATH`。默认构建不内嵌工具，不应将普通 release 称作完整截图免依赖包；bundleffmpeg 需要预先提供匹配目标平台的工具，体积由所用工具版本决定。

启动时探测 ffmpeg 版本并在设置页展示；缺失时截图功能禁用但其余功能可用。`seedkit doctor` 子命令输出同样的探测结果。
PNG 量化压缩改为可选且默认关闭：它是截图后的额外 CPU 阶段，4K 图片使用 256 色量化与 Floyd-Steinberg 抖动时耗时明显。相比默认开启以节省上传体积，优先缩短盒子任务时间；截图选项和设置页提示耗时与体积取舍。关闭压缩仍保留原分辨率 PNG；配置中用户明确保存的 `compress: true` 不在加载时强行覆盖。

## 管理员密码恢复

`seedkit passwd` 通过本机交互终端隐藏输入两次密码，按现有 bcrypt 规则替换 `auth.password_hash`，不接受密码参数。直接修改 YAML 节点而不调用带环境覆盖的 `config.Load`，防止把容器环境写回配置或丢失未知键。需要已有管理员；`SEEDKIT_AUTH_PASSWORD_HASH` 存在时拒绝操作，以免重启覆盖新密码。

运行进程持有内存中的凭据与会话，因此重置后必须重启；不增加 HTTP 恢复入口或文件监视器。代价是短暂重启，收益是恢复路径不依赖已遗忘的密码，也不扩大公网鉴权面。重置期间不要并发保存网页设置。

## 包拓扑

```
cmd/seedkit          main：子命令 serve / doctor / passwd / version
internal/config      YAML 配置 + 环境变量覆盖；数据目录布局
internal/server      HTTP 路由、embed 前端、SSE 事件流
internal/auth        单用户密码登录，Cookie 会话，登录失败限速
internal/task        任务队列；重任务（BDInfo/截图/制种）按配置并发互斥排队
internal/fsroot      os.Root 沙箱：根目录句柄表，所有读写经内核 beneath 检查，`..`/符号链接不可逃逸；输出目录用 Single
internal/media       go-mediainfo / go-bdinfo 封装，统一 Report 结构
internal/screenshot  移植 bd.sh：ffprobe 探测 → HDR tonemap 滤镜链 → 字幕烧录 → 量化压缩
internal/upload      图床客户端：picgo.net（Chevereto API，含 tEXt 去重）、pixhost
internal/torrent     mkbrr 薄封装（~100 行）
internal/bbcode      text/template 模板渲染，用户可自定义多套
internal/tools       外部工具探测与路径解析（ffmpeg/ffprobe）
web/                 Alpine.js + 原生 CSS，本地静态资产 web/static 由 web/embed.go 嵌入
```

任务是唯一跨模块通道：`fsroot`/`media`/`screenshot`/`torrent` 只暴露同步函数，`task` 包负责排队、取消（ctx）、进度事件；`server` 只把事件转成 SSE。`media` 包 **must** 接受 `context.Context`，因为 go-bdinfo 扫描一张原盘要分钟级。

包按领域组织，由 `server` 编排任务、领域服务和结果存储。`server` 拆为 `server.go`（路由/helpers/SPA）+ `handlers_fs.go` + `handlers_media.go` + `handlers_misc.go`，拆文件不拆包。单文件包（auth、bbcode、config、fsroot…）是 Go 常态，不为凑文件数拆分。

## 路径安全：os.Root 而非前缀匹配

最初 `files` 包用 `filepath.Join` + `strings.HasPrefix` 做包含检查，server 里 `/api/output/` 和 `/api/upload` 又各自重复一遍。问题：词法检查挡不住符号链接，盒子 `/data` 下一个指向 `/` 的软链就能读整个系统。

改为 Go 1.24+ 标准库 `os.Root`：每个根 `OpenRoot` 一次，所有操作走 `Root.Open/Stat/Mkdir/Rename/RemoveAll`，遍历用 `fs.WalkDir(root.FS())`，输出目录用 `http.FileServerFS(root.FS())` 直接服务。内核级保证，三处手工检查全部删除。否决第三方 `filepath-securejoin`（作者自己建议 1.24 后改用 os.Root）和 `afero`（为可替换 FS 设计，这里用不上）。

副作用：`Resolve` 返回的绝对路径仅用于传给 ffmpeg/bdinfo 子进程，返回前先经 Root.Stat 验证。`..` 在 Single（输出目录）里被钳在顶层而非报错，和 `http.FileServer` 行为一致。

## Alternatives considered

- **bdinfo-rs 固化二进制 exec**：内存最低（12-34 MB vs go-bdinfo 52-89 MB），许可证自由。否决：需要第二个二进制文件，违反无依赖安装；额外一套 Rust 工具链让 Docker 构建慢 5-10 分钟；exec 只能拿文本报告，拿不到进度。
- **自写 BitTorrent v1 制种（bencode + 并行 SHA-1，260 行）**：已实现并验证 infohash 与 mkbrr 一致。否决：mkbrr 已是库（`torrent` 包公开 `CreateTorrent`），自带 tracker 规则表、piece 长度 oracle、season pack 检测，零成本跟进。踩坑：`mkbrr.Create()` 返回的 `TorrentInfo.MetaInfo` 为 nil（v1.27.0），**must** 用 `CreateTorrent` 后自己 `Write`。
- **dotnetcorecorner/BDInfo（.NET self-contained）**：官方实现。否决：常驻 150 MB+，与占用低矛盾。
- **mediainfo CLI 外部依赖**：PT 站要的是它的原生文本格式。否决：go-mediainfo 输出格式已对齐官方，且与 go-bdinfo 同一维护团队。
- **ffmpeg 内嵌 WASM（go-ffmpreg）**：真正单文件，+7.5 MB。否决：ffmpeg 5.1 旧版，没有 libzimg 所以没有 zscale，HDR tonemap 质量倒退；单线程无 SIMD，4K HEVC 截一帧预计慢 5-10 倍。
- **ffmpeg-statigo 静态库 cgo**：原生速度、真单文件。否决：cgo 阻断交叉编译；要用 libav API 重写整条截图链（解码、tonemap、PGS 叠加），工作量是 exec 的 3-5 倍。
- **pngquant / libimagequant cgo 绑定**：效果最好。否决：cgo。截图是预览用途，Median Cut + Floyd-Steinberg 足够。
- **Rust 全栈**：内存再省 10 MB。否决：对 2 GB 盒子无意义，开发速度慢 2-3 倍，没有可借鉴的 Go 参考代码（minfo）。

## Consequences

- 收益：主程序可直接运行；默认裸机截图仍需提供 ffmpeg/ffprobe。Docker 基于 Alpine，安装 ffmpeg、CA、时区及中文字幕字体；空闲占用低是目标，不是未经实测的保证。
- 代价：需要遵守依赖和随包资源的许可；BDInfo 扫描与 PNG 量化有 CPU/峰值内存成本，不能把空闲占用当作重任务峰值。
- 约束：**never** 引入 cgo 依赖；**never** 在运行时从网络下载二进制；新增外部工具走 `internal/tools` 探测且缺失时功能降级而非启动失败。
