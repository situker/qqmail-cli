# qqmailctl v0.1–v0.3 开发总结

## 本次目标

本轮开发以 `TECHNICAL_PLAN.md` 为单一真相源，从已经完成的 v0.1 读取内核继续推进，在一个连续交付周期内完成：

- v0.1 本机可完成的兼容性探针与证据收尾。
- v0.2 的同步索引、检索、规则分类、备份与受保护清理闭环。
- v0.3 的安全发送、回复与转发。
- Agent 契约、Skill、文档、测试、发布准备与 owner 交接。

最终开发版本为 `0.3.0-dev`。本轮没有 push、正式 tag、公开发布、真实 SMTP 发送或真实邮箱写操作。

## 交付规模

截至 2026-09-01，仓库包含：

- 31 个可发现的叶子命令。
- 32 份嵌入式 JSON Schema。
- 80 个 Go 源文件，其中 32 个测试文件。
- 22 个 Go 包。
- Windows、macOS、Linux × amd64、arm64 共 6 个 snapshot 二进制。

数字只用于描述本次交付快照，不作为未来版本的稳定承诺。

## v0.1：读取与兼容性收尾

- 把 S1–S11 全部落为 `spikes/` 下可复跑 PowerShell 探针，并提供跨平台 Go 探针核心。
- 所有原始结果进入 Git 忽略目录；提交记录只保留脱敏结论。
- 完成 S1/S2/S3/S5 只读阶段/S6/S7 基线/S10 的 2026-09-01 实测观察。
- 验证 QQ 当前观测到 MOVE、UIDPLUS、ID、IDLE 和 587 STARTTLS；同时保留“不把日期观察写成服务商保证”的边界。
- 中文 SEARCH 的同步 literal 行为不合规，因此非 ASCII 主题/发件人筛选采用确定性客户端窗口过滤。
- S4、S5 写阶段、S7 第二快照、S8、S9、S11 保持 owner 门控，没有为了“完成度”冒险运行。

## v0.2：本地整理闭环

- 引入 `modernc.org/sqlite` v1.57.0，保持 `CGO_ENABLED=0`。
- 实现 WAL、`PRAGMA user_version` 编号迁移、进程文件锁、UID 水位和 UIDVALIDITY 重置。
- 实现 FTS5 与中文字符 bigram 影子列，支持增量 `sync` 和 `search --local`。
- 实现纯规则 `triage analyze/plan`，支持 TOML 扩展规则与消毒后的 Markdown 审阅表。
- `backup --plan` 复用 `.eml` 导出与 HMAC manifest，批量结果合并为单一可信备份记录。
- `clean --plan` 实现本地备份、服务器真相和可选全文比对三道门。
- IMAP 写调用集中在唯一边界；静态测试和方言线缆测试同时守卫“绝不裸 EXPUNGE”。
- S5 写探针未运行时，fallback 固定为 COPY → 确认副本 → STORE `\\Deleted`，不 expunge，并报告源夹保留。
- `watch --jsonl` 使用轮询而非 IDLE；缓存默认不保存正文或预览。
- 所有写尝试进入 SQLite audit 与内容无关的 JSONL。

## v0.3：安全发送

- 使用 `go-smtp` v0.25.0；QQ 默认优先 465 隐式 TLS，连接建立失败时回退 587 STARTTLS。
- 实现中文主题/显示名、中文附件名、quoted-printable 正文、base64 附件和 Bcc 隔离。
- `send`、`reply`、`forward` 默认 dry-run，完整展示待发摘要。
- 真实提交需要非空白名单覆盖全部 to/cc/bcc、真实 TTY、精确键入 `SEND`，并且 readonly 未启用。
- `reply` 维护 `In-Reply-To`/`References`；`forward` 引用原邮件并携带原附件，但不伪装为直接回复。
- 每次调用最多提交一封；限流响应映射为 `rate_limited` / 退出码 30，不自动重试。
- SMTP 审计不记录授权码、收件人、主题、正文或附件名。
- MCP 明确未实现。

## 工程与契约

- 既有 JSON 包络、schema version 和退出码语义只增不改。
- 新命令全部加入风险目录、schema 契约测试和 readonly 套件。
- 邮件主题、正文、发件人、附件名、引用体等路径同步进入不可信字段清单。
- 项目 Skill 覆盖读取、整理、发送纪律及完整退出码决策表，并通过 Skill Creator 校验。
- 代码按 Conventional Commits 小步提交，每个功能切片保持可构建。

## 验证证据

最终验证通过：

- `go test ./...`
- `go vet ./...`
- `golangci-lint run`：0 issues
- `govulncheck ./...`：No vulnerabilities found
- `CGO_ENABLED=0 go build`
- `scripts/smoke-ps51.ps1`
- Skill Creator `quick_validate.py`
- `goreleaser check`
- `goreleaser build --snapshot --clean`：6 个目标全部成功

真实 QQ 只读验证确认了授权保存、未读列表、正文 PEEK 不改未读状态和 PowerShell 5.1 中文 JSON 路径。完整脱敏证据见 `docs/compat/`。

## 保守决策与待办

- 没有专用测试邮箱授权时，不运行登录频率、COPY/EXPUNGE、文件夹写入或 SMTP 配额探针。
- 没有实测 S5 写结论时，不使用 UID EXPUNGE fallback。
- 每个 CLI 进程只发送一封，因此 2 秒内部间隔只为未来进程内批处理预留，不宣称跨进程限流。
- macOS Keychain、Linux Secret Service、新 IP 登录和 QQ 长时 IDLE 仍需真实环境补证。
- 正式版本号、公开仓库可见性、tag、Release 与包管理器登记均留给 owner。

验收步骤见 [2026-09-01 测试手册](TESTING.md)。开源步骤见 [2026-09-02 发布清单](RELEASE_CHECKLIST.md)。
