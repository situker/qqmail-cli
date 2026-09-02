# 2026-09-01 发布前测试手册

目标：在不触碰非测试邮件、不泄露授权码的前提下，为 2026-09-02 开源建立可复核的发布证据。

## 测试原则

- 自动化测试可以在仓库直接运行。
- 真实账号默认只跑只读和本地写测试。
- 服务器写入与真实发送只允许专用测试邮箱，并且只操作脚本自建文件夹和自发邮件。
- 不把账号地址、授权码、主题、正文、附件、消息 ID 或原始协议日志贴进 Issue、提交记录或本测试文档。
- 任一认证或限流错误出现后立即停止，不连续重试。
- `dist/`、`bin/`、`spikes/results/` 是本地产物，不提交仓库。

## A. P0 自动化质量门

在仓库根目录运行：

```powershell
$env:Path = 'C:\Program Files\Go\bin;' + $env:Path
$env:GOPROXY = 'https://goproxy.cn,direct'

go mod tidy
go test ./...
go vet ./...
golangci-lint run
govulncheck ./...
pwsh -File .\scripts\check-docs.ps1
```

通过标准：

- `go mod tidy` 后 `go.mod`/`go.sum` 没有意外漂移。
- 所有包测试通过。
- `go vet` 无错误。
- golangci-lint 输出 `0 issues.`。
- govulncheck 输出 `No vulnerabilities found.`。
- 文档检查输出 `qqmail-cli documentation checks passed`。

本轮测试覆盖 schema 契约、readonly 命令面、写调用静态边界、无裸 EXPUNGE 线缆守卫、MIME、白名单、审计脱敏和 panic 脱敏。

## B. P0 Windows 构建与冒烟

```powershell
$env:CGO_ENABLED = '0'
go build -o .\bin\qqmail-cli.exe .\cmd\qqmail-cli
.\bin\qqmail-cli.exe version --json
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke-ps51.ps1
```

通过标准：

- `version --json` 的 `data.version` 为预期开发或发布版本。
- `schema_version` 为 `"1"`。
- smoke 输出 `qqmail-cli PowerShell smoke test passed`。
- `agent-info` 包含 `read`、`mutate`、`destructive`、`send` 四种风险。

## C. P0 六平台 snapshot

```powershell
goreleaser check
goreleaser build --snapshot --clean
```

核对 `dist/artifacts.json` 中恰好存在：

- windows/amd64
- windows/arm64
- darwin/amd64
- darwin/arm64
- linux/amd64
- linux/arm64

Snapshot 名称由 GoReleaser 根据当前 commit 生成，不是正式版本 tag。

## D. P0 开源文件检查

```powershell
git status --short
git diff --check
git remote -v
git tag --list
```

通过标准：

- 工作区没有未提交代码或意外文件。
- 没有空白错误。
- `origin` 指向预期仓库。
- 正式发布前没有误打 tag。
- `LICENSE`、`NOTICE`、`README.md`、`README.en.md`、`SECURITY.md`、`CONTRIBUTING.md`、`CODE_OF_CONDUCT.md` 均存在。

再人工检查 Git 跟踪文件，确认不存在真实邮箱地址、授权码、真实邮件、私有附件、未脱敏日志或 owner token。

## E. P0 真实只读冒烟

先设置 PowerShell 5.1 UTF-8：

```powershell
$utf8 = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8
$env:QQMAIL_CLI_READONLY = '1'
```

### E1. 本地状态与连接

```powershell
.\bin\qqmail-cli.exe auth status --json | ConvertFrom-Json | Out-Null
.\bin\qqmail-cli.exe doctor --json | ConvertFrom-Json | Out-Null
.\bin\qqmail-cli.exe folder list --json | ConvertFrom-Json | Out-Null
```

通过标准：三个命令退出码均为 0，stdout 均能解析为 JSON。

### E2. 未读状态保持

```powershell
$before = .\bin\qqmail-cli.exe envelope list --folder INBOX --unread --limit 20 --json | ConvertFrom-Json
$target = $before.data.envelopes | Select-Object -First 1
if (-not $target) { throw '没有可用于 PEEK 验证的未读邮件' }

.\bin\qqmail-cli.exe message show $target.id --part text --json | ConvertFrom-Json | Out-Null
$after = .\bin\qqmail-cli.exe envelope list --folder INBOX --unread --limit 20 --json | ConvertFrom-Json
if ($after.data.envelopes.id -notcontains $target.id) { throw '目标邮件不再处于未读列表' }
```

再在 QQ 网页端刷新，确认目标邮件仍显示未读。不要把 `$target` 的内容复制进测试记录。

### E3. 附件元数据和轮询

对一封确认含附件的测试邮件运行：

```powershell
.\bin\qqmail-cli.exe attachment list $attachmentMessageId --json | ConvertFrom-Json | Out-Null
.\bin\qqmail-cli.exe watch --folder INBOX --jsonl --once
```

附件下载不是纯读本地操作；只有确实需要验证文件写入时才临时关闭 readonly，并使用独立临时目录。

## F. P0 本地索引与规则

`sync` 会写本地 SQLite，但不会修改服务器。人工确认本地缓存位置后运行：

```powershell
Remove-Item Env:QQMAIL_CLI_READONLY -ErrorAction SilentlyContinue
.\bin\qqmail-cli.exe sync --json | ConvertFrom-Json | Out-Null
.\bin\qqmail-cli.exe search "测试" --local --limit 20 --json | ConvertFrom-Json | Out-Null
.\bin\qqmail-cli.exe triage analyze --json | ConvertFrom-Json | Out-Null
.\bin\qqmail-cli.exe triage plan --output .\test-plan.json --markdown .\test-plan.md --json | ConvertFrom-Json | Out-Null
.\bin\qqmail-cli.exe cache inspect --json | ConvertFrom-Json | Out-Null
```

通过标准：

- `sync` 成功，UIDVALIDITY 变化只产生 warning 并重建对应文件夹索引。
- 中文搜索可执行。
- plan JSON 通过嵌入 schema。
- `cache inspect` 显示 `preview_rows=0`、`body_rows=0`，除非明确测试过缓存开关。
- Markdown 中没有可执行 ANSI、控制符或 bidi 覆盖效果。

测试产生的 `test-plan.json`/`test-plan.md` 不提交，可在检查后移到项目外或手动删除。

## G. P0 发送 dry-run

真实 SMTP 不在本节发生：

```powershell
.\bin\qqmail-cli.exe send --to owner-approved@example.com --subject "qqmail-cli dry-run" --body "No message should be sent." --json | ConvertFrom-Json | Out-Null
```

通过标准：

- 输出 `dry_run=true`、`sent=false`。
- 命令不连接 SMTP。
- 摘要包含 from/to/cc/bcc、主题、正文摘要和附件清单。
- 白名单状态如实显示；白名单为空不影响 dry-run，但会拒绝 `--execute`。

`reply`/`forward` dry-run 会读取原邮件，因此只选择专用测试邮件，并把输出当作不可信数据。

## H. P1 专用邮箱写测试

P1 不阻塞本地代码质量门，但在声称“真实写路径已验证”前必须完成。只允许专用测试邮箱。

### H1. 双重门

```powershell
$env:QQMAIL_CLI_E2E_WRITE = '1'
$env:QQMAIL_CLI_DEDICATED_TEST_ACCOUNT = '1'
```

依次运行 spike 探针的写阶段（频控 S4、写路径 S5、发送配额 S9、中文夹写 S11）；网页收取选项快照与新 IP 登录验证需要人工网页设置或另一台主机。

### H2. 真实发送

- 白名单只允许专用邮箱自己的地址。
- `QQMAIL_CLI_TEST_RECIPIENT` 必须与默认专用账号完全相同。
- 先 dry-run，再由人类在 TTY 中逐封键入 `SEND`。
- 首次错误立即停止，不重试。

### H3. 标已读、移动与 clean

- 只选择 S9 自发邮件或其他明确由测试流程制造的邮件。
- `mark-read` 和 `move` 先 dry-run，执行后同时用 CLI 与 QQ 网页确认。
- `clean` 必须生成只含测试邮件的 plan，先完成 `backup --plan`，再考虑 `--paranoid --execute`。
- 绝不触碰账号中原有邮件。

完成后清理环境变量：

```powershell
Remove-Item Env:QQMAIL_CLI_E2E_WRITE -ErrorAction SilentlyContinue
Remove-Item Env:QQMAIL_CLI_DEDICATED_TEST_ACCOUNT -ErrorAction SilentlyContinue
Remove-Item Env:QQMAIL_CLI_TEST_RECIPIENT -ErrorAction SilentlyContinue
$env:QQMAIL_CLI_READONLY = '1'
```

## 验收记录

| 项目 | 2026-09-01 状态 | 证据 |
|---|---|---|
| go test / vet / lint | 已由本地开发验收通过，发布前复跑 | 终端摘要，不提交敏感原文 |
| govulncheck | 已由本地开发验收通过，发布前复跑 | `No vulnerabilities found.` |
| PowerShell smoke | 已由本地开发验收通过，发布前复跑 | 固定成功标记 |
| 六平台 snapshot | 已由本地开发验收通过，发布前复跑 | `dist/artifacts.json` 六目标 |
| 真实只读 PEEK | 已有 2026-09-01 脱敏观察 | `docs/compat/qq-20260901.md` |
| 本地 sync/search/triage | 待 owner 按本手册复核 | 只记录通过/失败和脱敏错误码 |
| 发送 dry-run | 自动化契约已通过；待 owner 看摘要 | 不记录真实地址或正文 |
| 真实 SMTP/服务器写入 | 待专用邮箱人工触发 | `docs/compat/` 新增带日期脱敏记录 |

P0 全部通过、真实账号只读冒烟无误后，即可发布。
