# seedkit

面向 PT 盒子的轻量发种辅助工具。把文件浏览、MediaInfo / BDInfo、截图上传和制种放在同一个 Web 界面里，减少在文件管理器、终端和图床之间来回切换。

Go 后端内嵌 Alpine.js 静态界面，无 Node.js、Java 或 .NET 常驻进程，也没有 npm 构建步骤。MediaInfo、BDInfo、制种和 PNG 量化在 Go 进程内完成；截图调用 **ffmpeg 和 ffprobe**。Docker 镜像包含这两个工具，普通单二进制构建不包含。

## 能做什么

- **文件管理**：配置多个根目录，浏览、预览、创建目录、复制、移动、重命名和删除；支持可设置的 Home、路径跳转与右键任务入口。
- **媒体报告**：提取 MediaInfo 和蓝光 BDInfo，查看、复制和下载结果。
- **截图与上传**：选择截图数量、字幕与处理选项，下载图片或上传到已配置图床；PNG 256 色量化与 Floyd–Steinberg 抖动可选，默认关闭。
- **制种**：基于 mkbrr 生成 torrent，确认 tracker、source、piece size 等参数后提交。
- **任务与成果**：通过 SSE 接收进度，查看日志、取消任务；任务历史与成果索引分别管理，清理任务不会自动删除成果。
- **界面**：明暗主题、磨砂 / 玻璃风格、壁纸和可关闭的看板娘，静态资产本地提供，不依赖运行时 CDN。

工具不负责向 PT 站点自动发布。ISO / DVD 的处理能力也不应视为与 BDMV 目录完全一致，具体以当前界面支持的任务类型为准。

## Docker 部署

### 从源码构建

```sh
git clone https://github.com/wuyaos/seedkit.git
cd seedkit
# 先修改媒体挂载路径；示例默认仅允许从服务器本机访问 18080
docker compose -f build/docker-compose.example.yml up -d --build
docker compose -f build/docker-compose.example.yml logs --tail 100
```

Compose 示例的数据目录相对于文件位置，为 `build/data/`；正式部署建议改成明确的绝对路径。启动后通过 SSH 隧道或已配置的反向代理访问，首次打开页面创建管理员。

### 使用 CI 镜像

镜像发布在 `ghcr.io/wuyaos/seedkit`，首次版本标签为 `v0.1.0`。下列示例固定使用 `0.1.0`；请以 [Actions 发布结果](https://github.com/wuyaos/seedkit/actions)为准。

```sh
docker run -d --name seedkit --restart unless-stopped \
  -p 127.0.0.1:18080:8080 \
  -v /opt/seedkit/data:/data \
  -v /mnt/downloads:/media:rw \
  ghcr.io/wuyaos/seedkit:0.1.0
```

把 `/mnt/downloads` 换成自己的媒体目录。进入设置页添加根目录：别名 `media`，目录 `/media`。文件列表使用 `media/Movies` 这样的虚拟路径，不是服务器任意绝对路径。只需分析、截图和制种时，可将媒体挂载改为 `:ro`；此时不能修改源文件。

CI 当前构建 **linux/amd64** 镜像，包含 ffmpeg、ffprobe 和中文字幕字体。`latest` 跟随默认分支，`sha-<短提交号>` 标记具体提交，`vX.Y.Z` 标签生成版本镜像。生产部署建议固定版本或 digest，而不是依赖可变的 `latest`。

**不要把未经配置的服务直接暴露到公网。** 管理员初始化、反向代理、数据持久化和环境变量说明见[部署说明](docs/deployment.md)。根目录建议在网页管理；显式设置 `SEEDKIT_PATHS_ROOTS` 会在启动时覆盖配置文件中的列表。

## 不使用 Docker

构建所需 Go 版本见 [go.mod](go.mod)。先安装 ffmpeg / ffprobe，并确保在 `PATH` 中，或在设置中指定路径。

```sh
CGO_ENABLED=0 go build -trimpath -o seedkit ./cmd/seedkit
SEEDKIT_DATA=./data SEEDKIT_SERVER_LISTEN=127.0.0.1:8080 ./seedkit serve
# 在另一个终端检查外部工具
SEEDKIT_DATA=./data ./seedkit doctor
```

打开 `http://127.0.0.1:8080` 创建管理员。Windows 构建使用 `-o seedkit.exe`，环境变量按自己的终端语法设置。未安装 ffmpeg / ffprobe 时，不具备完整截图能力。

## 配置与数据

`SEEDKIT_DATA` 指定数据目录，主要包含：

| 路径 | 内容 |
| --- | --- |
| `config.yaml` | 管理员密码哈希、根目录、任务与图床设置 |
| `tasks.json` | 任务历史 |
| `results.json` | 独立的成果索引 |
| `output/` | 默认的截图、报告、torrent 等产物，可通过 `paths.output` 修改 |

升级时保留整个数据目录以及自定义输出目录。图床密钥、配置文件、cookie 和私有 tracker 地址不要提交到仓库；设置 API 返回的密钥已脱敏。支持的环境变量见[部署说明](docs/deployment.md#环境变量)。

Home、主题和任务勾选是浏览器本地偏好，不会随服务器数据跨设备同步。PNG 压缩虽能缩小图片，但高分辨率量化会增加 CPU 耗时；默认保留原分辨率 PNG、不执行量化。

### 重置管理员密码

```sh
# 和服务使用同一个数据目录；在交互终端中隐藏输入两次
SEEDKIT_DATA=./data ./seedkit passwd

# Docker
docker exec -it seedkit seedkit passwd
docker restart seedkit
```

只修改已有管理员的密码哈希，不清除设置或产物。重置后必须重启以加载新密码并使旧会话失效；期间不要同时在网页保存设置。如配置了 `SEEDKIT_AUTH_PASSWORD_HASH`，先移除此覆盖，否则重置会被拒绝。命令不接受明文密码参数。

## 开发与 CI

```sh
go test ./...
go vet ./...
node --check web/static/app.js
node scripts/test-task-preferences.cjs
node scripts/test-task-actions.cjs
python3 -m unittest discover -s scripts -p 'test_*.py'
make build
make release    # 交叉编译到 dist/；只包含主程序，不附带 ffmpeg
```

Node 与 Python 仅用于开发检查，不是运行时依赖。此 Windows 开发环境使用 `micromamba run -n base python` 代替上面的 `python3`。

[GitHub Actions 工作流](.github/workflows/ci.yml)在 PR 中执行测试与容器构建，但不推送镜像；默认分支推送、版本标签和手动运行通过测试后发布到 GHCR。发布使用 `GITHUB_TOKEN` 的 `packages: write` 权限，无需 Docker Hub 密钥。首次发布后需检查包的可见性，公开 GitHub 仓库并不保证已有 GHCR 包自动公开。

单测与 CI 不替代真实媒体、浏览器、字幕渲染、外部图床及服务器验收。[开发说明](docs/development.md)记录当前技术边界，[架构取舍](.agents/notes/implemented/architecture/2026-10-31-single-static-binary.md)记录设计理由。

## 参考项目与致谢

seedkit 在功能组合和操作流程上参考以下项目；它是独立实现的工具，不是这些项目的官方 Web 版，也不代表上游为本项目背书。

| 项目 | 参考内容 |
| --- | --- |
| [Kuanghom/BDInfo](https://github.com/Kuanghom/BDInfo) | 媒体信息、截图、上传、制种与 BBCode 的组合工作流，以及 bd.sh 的处理经验 |
| [YEAHZERO/BDInfoWebUI](https://github.com/YEAHZERO/BDInfoWebUI) | BDInfo 的 Web 操作流程、任务进度与结果呈现 |
| [mirrorb/minfo](https://github.com/mirrorb/minfo) | Go 媒体工具的能力组合与轻量部署思路 |

实际使用的库和静态资源另列如下，**致谢不替代许可证，也不改变上游版权**：

- [autobrr/go-bdinfo](https://github.com/autobrr/go-bdinfo)：BDInfo 扫描与报告。
- [autobrr/go-mediainfo](https://github.com/autobrr/go-mediainfo)：媒体分析与文本报告。
- [autobrr/mkbrr](https://github.com/autobrr/mkbrr)：torrent 创建。
- [ericpauley/go-quantize](https://github.com/ericpauley/go-quantize)：PNG 调色板量化；抖动使用 Go 标准库 `draw.FloydSteinberg`。
- [Alpine.js](https://github.com/alpinejs/alpine) 与 [Lucide](https://github.com/lucide-icons/lucide)：前端交互与图标。
- [PC2005-cloud/dsh-pet](https://github.com/PC2005-cloud/dsh-pet)：看板娘图片和动画。
- [FFmpeg](https://ffmpeg.org/)：截图、字幕与视频处理的外部工具；Docker 使用 Alpine 提供的包。

壁纸由项目维护者提供并确认具有公开分发权限；这不表示其他视觉资源或第三方代码被重新授权。

## 许可状态

seedkit 自有代码采用 **GPL-3.0-only**，完整文本见 [LICENSE](LICENSE)。程序不提供担保；你可以按该许可证使用、修改与再分发。第三方库、前端与素材仍保留各自的许可和版权声明，见 [第三方声明](THIRD_PARTY_NOTICES.md)。

BDInfo 已固定到 `v0.4.3-0.20260907152737-c4a4d2f7dd39`，对应上游 [PR #41](https://github.com/autobrr/go-bdinfo/pull/41) 明确的 GPL-2.0-or-later 授权，不再依赖缺少声明的旧版本。许可声明随源码和 Docker 镜像提供，镜像内位于 `/usr/share/seedkit/`。
