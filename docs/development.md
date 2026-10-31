# 开发说明

本文描述当前实现及验收路径，不代替代码，不新增全局 Agent 规则。

## 当前技术边界

- Go 后端；Alpine.js + 原生 CSS 的无构建前端，`web/embed.go` 嵌入 `web/static/`。旧 Vue/Vite/Naive UI 路线已移除，无需恢复 npm 清单或 `web/dist`。
- 静态资产全部本地提供，包含 Alpine、Lucide SVG sprite、壁纸和看板娘；不用运行时 CDN。看板娘视频有页面不可见时的暂停逻辑。
- 文件操作走 `internal/fsroot` 的 `os.Root`。页面和结果索引使用根目录别名下的虚拟路径，不把绝对路径拼接/字符串前缀校验当沙箱替代品。
- `internal/task` 处理排队、取消、进度与任务历史；SSE 驱动前端，不增加固定间隔轮询。`internal/results` 保存独立结果索引，清理任务不等于删除成果。
- 外部工具发现集中于 `internal/tools`；普通发布产物是主程序单二进制，截图完整能力仍取决于 ffmpeg/ffprobe。

## 修改前端

修改 `web/static/index.html`、`app.js`、`app.css` 后重建主程序，运行中的旧二进制不会自动读取新文件。JS 与 HTML 使用同一个本地 sprite 地址；sprite、HTML、JS、CSS 使用重新验证缓存。更新一年 immutable 的静态资产时需换资源 URL，不能只原地覆盖。

增加 Lucide 图标后运行：

```sh
# 此开发机的 Python 命令；脚本本身只依赖标准库，生成时需访问图标源
micromamba run -n base python scripts/icons.py
```

`node scripts/test-task-preferences.cjs` 使用 VM stub 验证浏览器任务偏好，不代替真实 DOM、Alpine 绑定和视觉检查。新增交互应实际确认键盘/鼠标路径、取消不提交、错误提示与禁用态；长报告、成组截图需要保留可见的底部操作栏。Home 和勾选项保持浏览器偏好，不偷偷写成服务器全局配置。

## 构建与检查

```sh
go test ./...
go vet ./...
node --check web/static/app.js
node scripts/test-task-preferences.cjs
CGO_ENABLED=0 go build -trimpath -o dist/seedkit ./cmd/seedkit
```

`make build`、`make docker`、`make release` 提供相同入口；Windows 可直接 `go build -o dist/seedkit.exe`。发布产物放 `dist/`；`make build` 的根目录临时程序也已忽略，运行数据不进 Git，Docker 构建上下文通过 `.dockerignore` 排除数据、历史、临时程序与发布包。不要把密钥、浏览器 cookie、测试媒体或服务器运行配置提交到仓库。

验收分开记录：单元测试/静态检查、浏览器真实交互、真实媒体任务、外部图床网络和服务器部署。测试没有覆盖或外部服务不可达时明确记录缺口，不用 HTTP 200 或产物文件存在当成任务完成。

## 已知边界

- ISO/DVD 不承诺与 BDMV 目录相同的处理能力；UI 按当前支持类型禁用不支持的任务。
- 外部图床的可用性、限额和密钥由供应商决定；测试上传会产生真实外部写入，需使用获准的目标。
- 并非所有包都有单元测试；当前测试集中在配置、文件沙箱、任务、CLI 与浏览器偏好，真实文件和浏览器检查仍不可省略。
- 发布前还需要核对 Go 依赖、ffmpeg 和随包视觉资源的许可与再分发要求；此说明不替代正式许可证。

重要架构取舍记录在 [.agents/notes/implemented](../.agents/notes/implemented)，只同步仍成立的决定与理由，不把聊天流水账搬入文档。
