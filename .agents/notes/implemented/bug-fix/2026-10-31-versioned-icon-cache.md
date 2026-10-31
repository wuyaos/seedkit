# Agent Note: 按内容版本缓存图标

Status: implemented

## Problem

真实浏览器操作中，外部 SVG use 引用在 DOM 更新后反复完整下载 sprite，单次传输约 14 KB。嵌入文件没有可用的修改时间，原 `no-cache` 策略未带来预期的轻量重新验证。

## Decision

图标 URL 使用 sprite 原始字节 SHA-256 的前 12 位作为 `v`。服务启动时只计算一次当前版本；只有请求版本与当前内容匹配时才返回一年 immutable 缓存。无版本、旧版本继续 no-cache，HTML、app.js 与 app.css 保持 no-cache。图标生成器识别带版本的引用，输出稳定的 UTF-8 字节，并同步 HTML 与 JavaScript 的全部引用。Go 测试检查嵌入内容与引用版本一致，防止手改 sprite 后漏换 URL。

此决定替代[文件导航与结果布局笔记](../feature/2026-10-31-file-navigation-and-results.md)中图标的 no-cache 决定；该篇导航和布局内容仍独立有效。[单二进制架构](../architecture/2026-10-31-single-static-binary.md)仅约束本地嵌入，未被替代。当前笔记树无其他重叠缓存提案。

## Alternatives considered

- 保留 no-cache 最容易保证更新立即可见，也是先前修复旧 Home 图标的理由；但浏览器实测会在操作时反复传输整个 sprite。
- 加 ETag 保留重新验证可缩小响应正文，但仍保留往返请求；内容版本地址同时解决旧图标与重复传输，且不需要运行时依赖。
- 手工版本号能复用缓存，但生成图标后容易忘记递增；生成器直接取内容摘要。

## Consequences

相同图标内容可以跨操作复用缓存，发布新内容会得到不同地址。启动新增一次约 14 KB 的摘要计算。修改图标必须通过生成器，或同步摘要引用并通过 Go 测试；不能只改 SVG。缓存收益仅限图标，不代表整个页面渲染或公网加载速度已经完成验收。

## Verification

`go test ./internal/server` 检查当前版本 immutable、旧版本与应用文件 no-cache、全部引用匹配；`micromamba run -n base python -X utf8 scripts/test_icons.py` 离线验证旧/无版本输入、内容变化与 URL 同步，不访问 CDN。真实浏览器检查操作前后 sprite 的 transferSize，作为传输复用证据。
