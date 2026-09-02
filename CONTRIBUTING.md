# Contributing to qqmail-cli

感谢参与 qqmail-cli。这个项目首先保护邮箱凭证和用户数据，其次才是功能扩展速度。

## 开始前

- 使用 Go 1.25 或更高版本。
- 阅读 `README.md`、`SECURITY.md` 和与改动相关的 `docs/`。
- Bug 使用仓库 Issue 模板，并只提供合成或脱敏数据。
- 安全问题不要开公开 Issue，按 `SECURITY.md` 私密报告。

## 本地开发

```text
go mod download
go test ./...
go vet ./...
golangci-lint run
govulncheck ./...
go build ./cmd/qqmail-cli
pwsh -File ./scripts/check-docs.ps1
```

Windows 还应运行：

```powershell
go build -o .\bin\qqmail-cli.exe .\cmd\qqmail-cli
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke-ps51.ps1
```

发 PR 前保证 `git diff --check` 通过。

## 代码边界

- go-imap 只能由 `internal/imapx` import。
- go-imap 写方法只能出现在 `internal/imapx/mutate.go`，且只能经 `internal/policy` 调用。
- 不得增加裸 `EXPUNGE`、永久删除命令、确认绕过 flag 或隐式发送。
- 读路径继续使用 `EXAMINE` 与 `BODY.PEEK`。
- 新写命令必须先检查 `QQMAIL_CLI_READONLY`，默认 dry-run，需要合理的 TTY 确认并写 SQLite + JSONL audit。
- 邮件派生字段必须标为 untrusted；人读渲染需要控制符、ANSI、bidi 和 Markdown 防护。
- CLI 本体不调用 AI 模型。

现有静态与线缆测试会守卫这些边界。不要删除测试来让不安全改动通过。

## 命令与契约

新增或改变命令时同时完成：

1. 在 command catalog 中登记叶子命令和 `read`/`mutate`/`destructive`/`send` 风险。
2. 增加嵌入式 data schema。
3. 增加命令级 `--json` 契约测试。
4. 更新 `agent-info` 的 untrusted paths。
5. 把所有写命令加入 readonly 全套测试。
6. 更新 README、使用手册、架构/安全文档和 Skill 中受影响的部分。

现有 JSON 字段、schema version 和退出码语义只能兼容性增加，不得静默删除或改义。

## 测试数据与真实邮箱

- 测试邮件必须是合成数据，不含真实地址、正文、附件或消息 ID。
- 授权码不得进入参数、日志、fixture、快照、CI、Issue 或 PR。
- 普通 CI 不连接真实 QQ 邮箱。
- 真实只读测试必须显式设置 E2E 开关并使用专用账号。
- 真实写测试需要 `QQMAIL_CLI_E2E_WRITE=1` 与 `QQMAIL_CLI_DEDICATED_TEST_ACCOUNT=1`，且只能操作探针自己制造的邮件和文件夹。
- 任何限流或认证失败出现后停止，不自动重试。
- 提交的兼容性结论必须脱敏、带绝对日期并写成“实测观察”，不能写成服务商保证。

## 依赖

新增依赖必须在 PR 中说明：用途、许可证、维护状态、为何标准库或现有依赖不能完成，以及可行的替代/回退。`github.com/emersion/go-imap/v2` 被有意锁定，升级属于单独迁移工程。

## 提交与 PR

- 使用 Conventional Commits，例如 `feat(index): ...`、`fix(send): ...`、`docs: ...`。
- 一个提交解决一个可审阅问题，提交后保持构建可用。
- 不混入格式化之外的无关改动。
- PR 描述写清：问题、方案、安全影响、契约影响、测试证据和文档变更。
- 不提交 `bin/`、`dist/`、`spikes/results/` 或本地 prompt/测试产物。

提交代码即表示你有权按仓库 Apache-2.0 许可证提供该贡献。
