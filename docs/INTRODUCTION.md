# qqmail-cli 项目介绍

qqmail-cli 是一个面向人类脚本与 AI Agent 的 QQ 邮箱安全优先命令行工具。它把标准 IMAP/SMTP 能力整理为稳定、可审计、默认保守的本地 CLI，让邮件读取、检索、整理、备份与发送都能进入自动化工作流。

qqmail-cli 是独立的第三方开源项目，与腾讯及 QQ 邮箱不存在隶属、合作或官方授权关系；项目通过用户主动开启的标准 IMAP/SMTP 服务工作。它也与 qmail 生态中的同名工具无关。

## 它解决什么问题

QQ 邮箱网页端适合人工操作，但不适合脚本和 Agent 稳定调用。直接操作 IMAP/SMTP 又有四类风险：

- 授权码容易进入命令历史、日志或配置文件。
- 高频登录、错误重试和不兼容的 SEARCH/IDLE 行为可能触发限流。
- 邮件正文、主题和附件名是不可信输入，可能污染终端或诱导 Agent 扩权。
- 批量整理与发送如果缺少备份、白名单和人工确认，误操作成本很高。

qqmail-cli 把这些风险变成代码级约束，而不是只写在注意事项里。

## 适合谁

- 希望在 PowerShell、Shell 或定时任务里检索和备份 QQ 邮件的人。
- 希望让 AI Agent 读取邮件，但不愿把授权码、删除权或发送权直接交给 Agent 的人。
- 需要把历史邮件建立本地索引、按规则分类并生成清理审阅计划的人。
- 需要可验证备份、稳定 JSON 契约和审计记录的自动化开发者。

## 能力概览

| 领域 | 能力 | 默认行为 |
|---|---|---|
| 读取 | 文件夹、信封、正文、附件元数据、批量读取 | `EXAMINE` + `BODY.PEEK`，不改变未读状态 |
| 本地检索 | SQLite 增量索引、FTS5、中文字符 bigram | 正文和预览默认不落盘 |
| 分类 | 内置规则、发件人域聚类、自定义 TOML 正则 | 完全本地、确定性、零 AI 调用 |
| 备份 | `.eml`、SHA-256、HMAC manifest、离线验证 | 可恢复、可复核、幂等续跑 |
| 整理 | 标已读、移动、计划化 clean | 默认 dry-run，真实 TTY 确认 |
| 监控 | UID 水位轮询、NDJSON 事件 | 默认 60 秒轮询，不依赖不稳定 IDLE |
| 发送 | send、reply、forward、中文 MIME、附件 | 默认 dry-run；白名单 + 人工 `SEND` |
| Agent 接入 | `agent-info`、嵌入式 JSON Schema、语义退出码 | 机器输出稳定，人读输出不作为契约 |

## 安全模型

```text
用户或 Agent
    │
    ├─ 只读命令 ──> IMAP Reader ──> EXAMINE / BODY.PEEK
    │
    ├─ 本地整理 ──> SQLite / FTS5 ──> plan / manifest / audit
    │
    ├─ 服务器写入 ─> policy ─> TTY 确认 ─> 唯一 IMAP mutation 边界
    │
    └─ 邮件发送 ──> allowlist ─> TTY SEND ─> SMTP TLS
```

核心约束：

1. 授权码进入操作系统凭据管理器，不进入仓库或普通配置文件。
2. `QQMAIL_CLI_READONLY=1` 会在取凭证和联网前阻断所有 mutate、destructive 与真实 send 操作。
3. `clean --execute` 先验证 manifest HMAC、本地 `.eml` 哈希和服务器 UIDVALIDITY/大小/Message-ID；`--paranoid` 再比对全文 SHA-256。
4. 项目没有面向用户的永久删除命令，任何协议路径都禁止裸 `EXPUNGE`。
5. 全部收件人必须命中发送白名单；Bcc 不进入 MIME 头；真实发送必须由人在 TTY 中确认。
6. 邮件派生字段在 JSON 中保持数据保真，在人读确认面剥离控制符、ANSI 与 bidi 覆盖符。

## 明确边界

- 不是 QQ 邮箱官方客户端，也不承诺服务器行为长期不变。
- 不抓取 QQ 网页 Cookie，不绕过授权码机制。
- CLI 本体不调用任何 AI 模型。
- 不提供永久删除、确认绕过或裸 SMTP 批量群发能力。
- 不实现 MCP；是否增加 MCP 留给后续独立决策。
- 兼容性结论只写成带日期的实测观察，不写成腾讯保证。

## 当前状态

截至 2026-09-01，代码版本为 `0.3.0-dev`。本地自动化验证、漏洞扫描、PowerShell 5.1 冒烟和六平台交叉构建均已通过；真实服务器写入和真实 SMTP 发送仍只允许 owner 在专用测试邮箱上按双重门手动验证。

从 [使用手册](USER_GUIDE.md) 开始实际使用；参与开发前阅读 [贡献指南](../CONTRIBUTING.md) 和 [安全策略](../SECURITY.md)。
