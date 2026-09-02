# qqmail-cli 架构说明

## 设计目标

qqmail-cli 是单进程、单二进制、本地优先的邮件 CLI。设计优先级依次是：凭证安全、不可误删、可审计、机器契约稳定、QQ 方言兼容、跨平台可构建。

## 组件关系

```text
cmd/qqmail-cli
    │
    ▼
internal/cli ─────────────── schemas/
    │                           │
    ├─ account / secrets        └─ 嵌入式 JSON 契约
    ├─ imapx Reader ──────── QQ IMAP
    ├─ syncer / index / triage
    ├─ exporter / cleaner
    ├─ policy ── imapx Mutator
    ├─ sendmail ───────────── QQ SMTP
    └─ output / errmap
```

`internal/cli` 只编排参数、连接和输出。协议、数据、策略和渲染分别放在独立包中，避免命令函数直接散落高风险调用。

## 读取路径

1. `account` 解析账号配置。
2. `secrets` 从操作系统凭据管理器取授权码。
3. `imapx.Reader` 建立严格 TLS 会话，登录后重新抓 CAPABILITY。
4. 文件夹只用 `EXAMINE`，正文只用 `BODY.PEEK`。
5. `mimeparse` 用 go-message 为主、enmime 为 fallback，统一 charset、HTML 清洗和资源上限。
6. `output` 生成文本或稳定 JSON 包络。

`internal/readonlyaudit` 的 AST 测试保证 go-imap 只被 `internal/imapx` 引用，并检查读取协议仍包含只读选择与 PEEK。

## 本地索引

SQLite 由纯 Go `modernc.org/sqlite` 提供，运行时启用 WAL、5 秒 busy timeout 与 foreign keys。写打开需要进程文件锁，同一账号只允许一个写进程。

Schema v1：

| 表 | 用途 |
|---|---|
| `folders` | 文件夹名、delimiter、UIDVALIDITY、UIDNEXT、同步时间 |
| `messages` | 信封、flags、分类头、可选预览/正文和搜索 bigram |
| `messages_fts` | FTS5 外部内容索引 |
| `sync_state` | 每文件夹 `last_seen_uid` 水位 |
| `audit` | 写操作命令、动作、ID、结果与 plan 引用 |

迁移使用 `PRAGMA user_version`。UIDVALIDITY 变化时删除该文件夹索引并重同步，同时输出 warning；不会继续使用旧 UID。

中文检索由应用生成字符 bigram 影子列，避免依赖 SQLite tokenizer 对中文分词的环境差异。

正文和预览默认是 SQL NULL。只有 `sync --cache-previews` 或 `--cache-bodies` 显式开启时才保存未加密内容。

## 分类与计划

`triage` 读取本地 messages：

- 先应用用户 TOML 正则规则。
- 再应用 List-Unsubscribe、Precedence、noreply、验证码、域名、年龄、大小和未读率等内置规则。
- 生成 schema-valid `plan.json` 与可选 Markdown 审阅表。

规则引擎完全确定性，不调用模型。Markdown 渲染会移除控制符、ANSI 与 bidi 字符，并转义 Markdown 标点。

## 备份与 clean

```text
plan.json
   │
   ▼
backup --plan ──> .eml + manifest(HMAC/SHA-256)
   │
   ▼
clean --plan
   ├─ Gate 1: HMAC + 本地文件哈希
   ├─ Gate 2: UIDVALIDITY + RFC822.SIZE + Message-ID
   ├─ Gate 3: --paranoid 全文 SHA-256
   ├─ TTY 精确数量确认
   └─ policy → MOVE 或保守 COPY/STORE
```

任一消息未过门，整个 clean 队列拒绝进入 mutation。服务器不声明 MOVE 时，当前 fallback 是 COPY、确认目标副本、对指定 UID STORE `\\Deleted`，不调用 expunge，并明确报告源记录保留。

## 写入边界

所有 go-imap 写方法只允许出现在 `internal/imapx/mutate.go`，且只允许 `internal/policy` 调用暴露的 mutation 接口。

防线包括：

- AST import 与方法调用守卫。
- command catalog 风险白名单。
- `QQMAIL_CLI_READONLY` 前置拒绝。
- dry-run 与真实 TTY 确认。
- SQLite + JSONL 双审计。
- 方言模拟器捕获完整协议流程，并拒绝任何含 `EXPUNGE` 的下发。

项目没有用户可见的永久删除语义。

## 发送路径

```text
Draft
  ├─ header/recipient/resource validation
  ├─ RFC 2047/2231 MIME build
  ├─ full dry-run summary
  └─ execute
       ├─ readonly gate
       ├─ all-recipient allowlist
       ├─ TTY SEND
       ├─ content-free audit attempt
       ├─ SMTP 465 TLS / 587 STARTTLS
       └─ content-free audit result
```

465 连接或 TLS 建立失败时才回退 587；认证或投递失败不跨端口重试，避免不确定投递后的重复发送。每次命令只生成一个 SMTP envelope。

## 输出与 Agent 契约

普通命令的 JSON 包络固定包含：

```json
{
  "schema_version": "1",
  "command": "...",
  "ok": true,
  "data": {},
  "error": null,
  "warnings": [],
  "meta": {}
}
```

`watch --jsonl` 是事件流例外，每行独立匹配事件 schema。`agent-info` 是命令面、风险级别、readonly 状态和不可信路径的发现入口。

JSON 保存邮件字段原值以维持数据契约；人读输出统一经过消毒。stderr 承载诊断与确认，`--json` stdout 保持机器纯净。

## 构建与发布

GoReleaser 设置 `CGO_ENABLED=0`，交叉构建 Windows/macOS/Linux 的 amd64/arm64。正式 tag 触发 GitHub Actions Release，生成归档、SHA-256 checksums、SPDX SBOM 与 build provenance。
