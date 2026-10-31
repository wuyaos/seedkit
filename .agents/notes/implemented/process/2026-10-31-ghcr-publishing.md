# Agent Note: GHCR 发布入口与首次公开边界

Status: implemented

## Problem

本地开发版本需要可重复的测试和 Docker 发布入口，但直接公开会同时分发静态链接的库、前端资产与壁纸。参考项目的致谢、GitHub 的许可证识别结果和明确的再分发授权不是同一件事。

## Decision

工作流使用 GitHub Actions，测试通过后构建 linux/amd64 镜像，目标为 GHCR；仅非 PR 事件登录并推送，使用作业级 `packages: write` 和内置 `GITHUB_TOKEN`。PR 运行测试及容器构建，不接触镜像推送凭据。Action 固定到已核对的提交，版本来自镜像 metadata，默认分支更新 latest，另提供提交与版本标签。

README 区分功能参考、实际依赖与素材来源，不暗示上游官方合作，也不以致谢代替许可。壁纸的公开分发权限已由维护者确认。

主项目采用 GPL-3.0-only。BDInfo 固定到 `v0.4.3-0.20260907152737-c4a4d2f7dd39`，对应上游已合并的 [PR #41](https://github.com/autobrr/go-bdinfo/pull/41)：README、源码 SPDX 与 NOTICE 明确 GPL-2.0-or-later。第三方代码与素材保留原许可，`licenses/` 随源码和镜像分发，旧 v0.4.2 包缺失声明不再作为当前依赖状态。

## Alternatives considered

- Docker Hub：使用普遍，但需要额外管理仓库凭据；维护者选择 GHCR 以复用 GitHub 权限和源码关联。
- 直接标记 GPL-3.0 并发布：流程最短，但不能由缺失的授权推导完整兼容性；不采用。
- 首发同时加入多架构：能覆盖 ARM 设备，但当前交付目标是 amd64 盒子，跨架构验证尚无证据；本轮不扩大范围。

## Consequences

测试与发布入口可重复执行，运行服务无需新增常驻依赖。当前许可缺口通过固定到明确授权的提交解决；代价是暂时使用伪版本。首次发布仍需检查公开内容，并验证 Actions、GHCR 可见性和真实镜像启动；本地检查不代替这些验收。
