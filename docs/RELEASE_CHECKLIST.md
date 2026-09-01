# 2026-09-02 开源发布清单

目标：把已经通过 2026-09-01 验收的代码安全地公开并发布首个正式版本。所有 GitHub 可见性、push、tag 和 Release 操作由 owner 执行。

## 1. 冻结发布候选

- [ ] 2026-09-01 P0 测试全部通过。
- [ ] 决定正式版本号。当前候选为 `v0.3.0`；未拍板前不要打 tag。
- [ ] `CHANGELOG.md` 的 Unreleased 内容与实际功能一致。
- [ ] `cmd/qqmailctl/main.go` 的开发默认值和 GoReleaser tag 注入策略已理解。
- [ ] 不再合入与发布无关的功能。

## 2. 仓库卫生

```powershell
git status --short
git diff --check
git log --oneline --decorate -12
git remote -v
git tag --list
git log --all --format="%an <%ae> | %cn <%ce>" | Sort-Object -Unique
```

- [ ] 工作区干净。
- [ ] `origin` 指向 `https://github.com/situker/qqmailctl.git`。
- [ ] `_internal/`、`bin/`、`dist/`、`spikes/results/` 未被 Git 跟踪。
- [ ] 没有真实邮箱地址、授权码、邮件正文、私有附件、未脱敏日志或 token。
- [ ] **提交元数据（author/committer）全部为 `situker@users.noreply.github.com`**——文件内容干净不等于提交历史干净，最后一条命令的输出必须只有 noreply 身份。
- [ ] 没有错误 tag 或意外大文件。

可用 `git ls-files` 人工复核完整公开清单。不要运行会把凭据值打印出来的环境枚举命令。

## 3. 开源入口文件

- [ ] `README.md` 和 `README.en.md` 能让新用户理解定位、安装、风险与快速开始。
- [ ] `LICENSE` 为 Apache-2.0，`NOTICE` 存在。
- [ ] `SECURITY.md`、`CONTRIBUTING.md`、`CODE_OF_CONDUCT.md` 已检查。
- [ ] Issue 模板、PR 模板和 Dependabot 配置存在。
- [ ] 第三方非隶属声明同时出现在 README、docs 首页和 GoReleaser release footer。
- [ ] `docs/` 导航能到达介绍、使用、测试、安全、发送、兼容性和发布文档。

## 4. GitHub 仓库设置

仓库已经配置本地 `origin`，但本地尚无已抓取的 `origin/main` 远端引用。owner 在公开前核对：

- [ ] 仓库所有者与名称正确。
- [ ] 先以 private 状态推送并等待 CI，通过后再切换 public；或确认公开动作与首推顺序。
- [ ] 默认分支为 `main`。
- [ ] Branch protection/ruleset 要求 CI 通过，并禁止直接强推或删除 `main`。
- [ ] Issues 已启用；按需要启用 Discussions。
- [ ] Private vulnerability reporting 已启用。
- [ ] Actions 权限足以创建 Release、上传 checksums/SBOM 并生成 provenance。
- [ ] 仓库简介、topics 与官网链接不使用 QQ/Tencent 官方措辞或视觉资产。

## 5. 最终质量门

完整执行 [测试手册](TESTING.md) 的 A–D：

```powershell
go test ./...
go vet ./...
golangci-lint run
govulncheck ./...
pwsh -File .\scripts\check-docs.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke-ps51.ps1
goreleaser check
goreleaser build --snapshot --clean
```

- [ ] 所有命令成功。
- [ ] Snapshot 六目标齐全。
- [ ] Windows amd64 二进制在干净终端中能运行 `version --json` 和 `agent-info`。
- [ ] Skill Creator `quick_validate.py` 通过。

## 6. 推送 main

由 owner 确认远端状态后执行：

```powershell
git push -u origin main
```

- [ ] GitHub CI 的 Windows、macOS、Linux 测试通过。
- [ ] lint 与 govulncheck jobs 通过。
- [ ] GitHub 页面正确识别 Apache-2.0 License。
- [ ] README 图片、相对链接和文档链接无 404。

如果远端已有历史，不要强推。先 fetch 并人工处理差异。

## 7. 创建正式 tag 与 Release

以下以 `v0.3.0` 为候选示例，版本未拍板时不要执行：

```powershell
git tag -a v0.3.0 -m "qqmailctl v0.3.0"
git push origin v0.3.0
```

tag push 会触发 `.github/workflows/release.yaml`。

- [ ] Release workflow 成功。
- [ ] 六平台归档全部出现。
- [ ] `checksums.txt` 存在且可验证。
- [ ] 每个归档有对应 SPDX SBOM。
- [ ] GitHub build provenance attestation 可见。
- [ ] Release notes 含非官方/非隶属声明与安全提醒。
- [ ] 随机下载一个 Windows 和一个非 Windows 归档，核对校验和及 `version --json`。

已经公开的 tag 不要改写或复用。发布失败时修复后创建新的补丁版本。

## 8. 开源公告素材

一句话介绍：

> qqmailctl 是一个安全优先的非官方 QQ 邮箱 CLI：支持只读检索、本地分类、可验证备份、受保护清理和白名单发送，并为 AI Agent 提供稳定 JSON 契约。

公告应说明：

- 这是独立第三方项目，不是 QQ 邮箱官方产品。
- 授权码只进操作系统凭据管理器。
- 默认 dry-run、Agent readonly、无永久删除命令。
- 支持 Windows/macOS/Linux 双架构。
- 真实服务器行为以带日期兼容性记录为准。

不要在公告截图中展示真实邮箱地址、主题、正文、附件名、消息 ID 或授权码。

## 9. 发布后 24 小时

- [ ] 观察 CI、Release 下载和首批 Issue。
- [ ] 认证或限流问题先请求脱敏错误码，不索取原始邮件或授权码。
- [ ] 严重安全问题转入私密报告渠道。
- [ ] 文档错误直接修复；协议兼容性问题先增加脱敏夹具和回归测试。
- [ ] 包管理器占位、winget/Scoop 等渠道作为独立 owner 任务，不阻塞 GitHub 开源。

完成公开发布后，把 `IMPLEMENTATION_STATUS.md` 中“未发布”状态更新为实际 tag 和 Release 日期，并把 `CHANGELOG.md` 的 Unreleased 内容归入该版本。
