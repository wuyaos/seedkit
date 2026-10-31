# 部署与运维

## 裸机

主程序以 `CGO_ENABLED=0` 构建，不需要 Node.js、Java、.NET、mediainfo CLI 或外部 mkbrr。默认构建仍依赖 ffmpeg/ffprobe 完成截图，可放在 PATH 或通过配置指定绝对路径；缺失不阻止主服务启动。先运行 `seedkit doctor` 检查探测结果。

构建标签 `bundleffmpeg` 提供内嵌工具的可选通道，需要先把匹配目标系统的静态工具放入 `internal/tools/bundled/ffmpeg` 和 `internal/tools/bundled/ffprobe`；仓库不附带这些文件，也不在启动时下载。普通 `make release` 不启用该标签，不应把默认发布包称为完整截图免依赖包。

## Docker

仓库 [Dockerfile](../build/Dockerfile) 使用多阶段构建：Go 编译主程序，Alpine 运行阶段只装 ffmpeg、CA、时区数据和文泉驿字体。不需要在服务器上安装 Go。

```sh
docker compose -f build/docker-compose.example.yml up -d --build
docker exec seedkit seedkit doctor
curl -fsS http://127.0.0.1:18080/api/status
```

编辑 [Compose 示例](../build/docker-compose.example.yml) 的媒体挂载路径后再启动。数据目录和媒体目录分开挂载；只需读取媒体时把 `rw` 改为 `ro`，文件重命名、移动、删除等功能会因此不可用。不要把整个宿主机根目录挂进去。

示例端口绑定 `127.0.0.1:18080`。从个人电脑可使用 SSH 隧道：

```sh
ssh -L 18080:127.0.0.1:18080 your-server
```

公网访问请在前面放 HTTPS 反向代理，允许 SSE 长连接并关闭其响应缓冲，再按实际需求调整端口绑定和防火墙。首次管理员创建没有邀请机制，不要把尚未初始化的服务直接暴露给互联网。Web 应用只提供 HTTP，本身不终结 TLS。

## 环境变量

启动时环境覆盖 YAML，不是所有字段自动映射。实际支持：

| 变量 | 含义 |
| --- | --- |
| `SEEDKIT_DATA` | 数据目录，默认 `./data` |
| `SEEDKIT_SERVER_LISTEN` | HTTP 监听地址，默认 `:8080` |
| `SEEDKIT_AUTH_USERNAME` | 管理员用户名 |
| `SEEDKIT_AUTH_PASSWORD_HASH` | bcrypt 哈希覆盖，不是明文密码 |
| `SEEDKIT_PATHS_ROOTS` | 根目录映射，如 `media=/media,downloads=/downloads` |
| `SEEDKIT_PATHS_OUTPUT` | 输出目录 |
| `SEEDKIT_TASKS_HEAVY_CONCURRENCY` | 重任务并发数 |
| `SEEDKIT_TOOLS_FFMPEG` / `SEEDKIT_TOOLS_FFPROBE` | 工具路径 |
| `SEEDKIT_UPLOAD_PICGO_API_KEY` | PicGo API 密钥 |
| `SEEDKIT_TORRENT_ANNOUNCE` | 默认 tracker announce |

其余设置通过网页或 `config.yaml` 修改。变更环境变量后重建/重启容器；保存网页设置不会移除容器的环境覆盖。

## 更新与回退

1. 暂停提交任务，等待运行中的任务结束；正常停服务。
2. 备份整个数据目录和自定义输出目录，保留旧镜像或旧二进制。
3. 构建并启动新版本；检查 `doctor`、登录、目录列表、历史结果和一个小文件的实际任务。
4. 若失败，停止新版本，恢复旧程序；如新版本修改了配置格式，同时恢复更新前的数据备份。

不要在运行时删除 `config.yaml`、`results.json` 或输出目录来“清理缓存”。删除任务只清任务历史；删除结果记录时是否同时删产物由显式选项决定。密码恢复见 [README](../README.md#重置管理员密码)。

## 排障

- 截图慢：查看任务日志中的耗时分段；先比较关闭 PNG 压缩与字幕时的耗时。原盘/远程盘读取、HDR tonemap、PGS/文本字幕及 4K 量化各有成本，不能都归因于网络。
- 补传失败：先看补传请求是否成功入队，再看任务里的具体图床错误。已有本地截图无需重新截取；补传不应覆盖已成功的链接。
- `Failed to fetch`：检查浏览器网络面板、服务是否重启、反向代理与 TLS。它是浏览器传输错误，不是可用于定位图床根因的服务端日志。
- 文件操作失败：核对根目录映射、宿主机权限和挂载是否只读。不要扩大为宿主机根挂载来绕过。
