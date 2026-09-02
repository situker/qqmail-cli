# qqmail-cli 功能与设计全景

> 本文是 qqmail-cli 的完整功能与设计参考，一篇读懂"它能做什么、怎么做到的、为什么这样设计"。面向想全面了解项目的使用者、贡献者与技术读者。

qqmail-cli 是独立的第三方开源项目，与腾讯及 QQ 邮箱不存在隶属、合作或官方授权关系；项目通过用户主动开启的标准 IMAP/SMTP 服务工作。与 qmail 生态的 qmailctl 工具无任何关联。

---

## 1. 一句话定位

**一个敢把 QQ 邮箱交给脚本和 AI Agent 的命令行工具。**

它解决的不是"能不能连上邮箱"——这个市面上不缺工具——而是连上之后那些没人认真对待的事：凭证会不会泄露、Agent 会不会误删邮件、邮件正文会不会反过来指挥 Agent、批量操作会不会失控。qqmail-cli 把这些约束写进代码，并用跑在 CI 里的守卫测试证明它们成立，而不是写在"注意事项"里。

## 2. 它解决的四类真实风险

把邮箱接进自动化，难点集中在四处：

1. **凭证泄露面**：授权码容易进命令历史、日志、配置文件、错误信息、崩溃栈。
2. **风控触发面**：高频登录、错误重试、不兼容的 SEARCH/IDLE 行为都可能触发 QQ 限流。
3. **内容污染面**：邮件正文、主题、附件名是不可信输入，可能污染终端或诱导 Agent 扩大权限。
4. **误操作面**：批量整理与发送若缺备份、白名单、人工确认，误删误发代价极高。

qqmail-cli 对这四面的回应贯穿全部设计，下文逐一展开。

## 3. 完整功能地图

能力按"读 → 理 → 发"三阶段组织，每阶段的安全默认值都比功能本身更被优先设计。

### 3.1 读（读取内核）

| 命令 | 作用 | 安全默认 |
|---|---|---|
| `auth login` | 引导式配置账号，隐藏输入授权码 | 验证连接成功后才写入系统凭据管理器 |
| `auth status` / `auth logout` | 查看/删除本地凭证引用 | 凭证只在凭据管理器，配置文件仅存引用 |
| `account list` / `account use` | 多账号管理 | 配置文件无任何秘密 |
| `doctor` | 一条命令诊断配置/凭证/TLS/登录/服务器能力 | 把"填了 QQ 密码而非授权码"等常见坑翻译成人话 |
| `folder list` | 枚举文件夹 | 自动解码中文文件夹的 Modified UTF-7 名 |
| `envelope list` | 列信封（轻量摘要），支持未读/发件人/主题/时间过滤 + UID 游标翻页 | `EXAMINE` + `BODY.PEEK`，不改未读状态 |
| `message show` | 读全文（纯文本/清洗 HTML/原始三档），支持一次批量多封 | 邮件内容按不可信数据处理，人读面剥离控制符 |
| `attachment list` / `download` | 附件元数据 / 显式下载 | 先元数据后下载；文件名防路径穿越、不覆盖 |
| `export` | 全量 `.eml` 备份 + SHA-256 + HMAC 签名清单 | 纯读操作；`--verify` 离线校验备份完整性 |

设计要点：`envelope`（信封摘要）与 `message`（全文）分离，Agent 先列摘要再按需批量取全文，既省 token 又天然只读。`message show` 接受多个 id 一次取回，直接回应 QQ 的登录频控——每次 CLI 调用就是一次 IMAP 登录，批量动词是减少登录次数的结构性手段。

### 3.2 理（本地索引、分类、门禁清理）

| 命令 | 作用 | 安全默认 |
|---|---|---|
| `sync` | 增量同步元数据进本地 SQLite 索引 | 正文/预览默认不落盘；升序分批提交水位，可断点续传 |
| `search --local` | 本地全文检索（含中文） | FTS5 + 字符 bigram 分词，纯本地零联网 |
| `triage analyze` / `plan` | 规则分类 + 生成清理审阅计划 | 本地确定性规则，CLI 永不调用 AI 模型 |
| `backup --plan` | 计划内邮件全量备份 + 校验 | 清理的前置门禁，备份验证不过不许清理 |
| `clean --plan` | 计划化清理，默认 dry-run | 三道门 + 人工 TTY 键数确认，只移入回收站 |
| `restore --plan` | 后悔药：从回收站整单找回原文件夹 | 按 Message-ID + 大小定位；回收站被清也有本地备份 |
| `message mark-read` / `move` | 标已读 / 移动 | dry-run 默认 + 精确数量确认 + 审计 |
| `watch --jsonl` | 新邮件事件流 | 轮询（不用不稳定的 IDLE），UID 水位去重 |
| `cache inspect` / `clear` | 检查 / 彻底清除本地索引 | `clear` 删整个库文件，保留不含内容的审计 JSONL |
| `audit list` | 读本地写操作审计 | 审计内容零泄露（只记命令/动作/结果，无正文收件人） |

**分类的防误删设计**（这是全项目最被打磨的一块）：`triage plan` 默认**不会**把整个邮箱做成可执行清理计划。默认范围——只纳入清理类别（营销/机器通知/社交通知），只扫当前文件夹（默认收件箱），只收 30 天以上、置信度 ≥0.8 的邮件；**星标邮件绝对排除，没有任何开关能放行**；交易邮件（订单/收据/发票/对账单等，即使挂着退订头）与政府域名（`*.gov.cn`）优先于营销判定被保护。被排除的数量和原因逐项可见。个人可用 `--rules` TOML 把业务相关发件人写成 `keep` 永久保护。

**清理的三道门**（`clean --execute` 前逐封校验，全部通过才放行，任何一封不过整批拒绝）：

1. **本地自洽**：备份清单 HMAC 验签 + 本地 `.eml` 哈希核对。
2. **服务器真相**：逐封核对 UIDVALIDITY / RFC822.SIZE / Message-ID（`--paranoid` 再加全文 SHA-256 重取比对）。
3. **人工确认**：真实 TTY 键入邮件总数——Agent 的管道喂不进去，且**不存在任何 bypass flag**（这一条本身有测试守着）。

清理动作只做"移入回收站"，**没有永久删除命令**，协议层守卫连 `EXPUNGE`/`CLOSE` 的下发路径都封死。

### 3.3 发（安全发送）

| 命令 | 作用 | 安全默认 |
|---|---|---|
| `send` | 构建并发送邮件 | dry-run 默认，完整展示信封后才能真发 |
| `reply` / `forward` | 回复 / 转发（自动带线程头、引用、附件） | 与 send 同门禁 |

真实发送必须：`--execute` + 账号配置了非空收件人白名单 + 全部收件人命中白名单 + 人工在真实 TTY 键入 `SEND`。每次调用最多一封，无 bypass。发送前对收件人/主题做 CRLF 头注入校验；内置保守节流应对 QQ 发送限制。

### 3.4 Agent 协议

| 命令 | 作用 |
|---|---|
| `agent-info` | 机器可读能力清单：全部命令与风险级别（read/mutate/destructive/send）、不可信字段路径、当前只读状态 |
| `schema [command]` | 输出内嵌 JSON Schema，Agent 消费任何形状前先自校验 |
| `version` / `completion` | 版本信息 / shell 补全 |

配套 `skills/qqmail-cli/SKILL.md`：随仓分发的 Agent 技能文件，装完 CLI 即获得完整调用纪律（Claude Code 等 harness 直接可用）。`QQMAIL_CLI_READONLY=1` 一个环境变量把整个写面锁死；未知取值一律按只读处理（fail-closed）。

## 4. 安全模型：七道防线，全部有测试背书

这是项目的核心。每一道都不是文档承诺，而是跑在 CI 里的守卫测试。

| 层 | 机制 | 验证方式 |
|---|---|---|
| 凭证 | 系统凭据管理器；env 注入需显式 `--auth-code-env`；全通道脱敏含 panic 栈 | 夹具授权码全输出扫描测试 |
| 只读 | 打开文件夹一律 EXAMINE、抓正文一律 BODY.PEEK；读接口方法白名单 | 接口反射测试 + 内存 IMAP 服务器行为断言（PEEK 后未读数不变） |
| 写边界 | go-imap 写方法圈禁在单一文件、仅策略层可调 | `go/ast` 静态扫描跑在 CI |
| 不可删 | 无永久删除命令；EXPUNGE/CLOSE 全域禁止 | AST 禁用表 + 协议线路断言 |
| 清理门禁 | 备份 HMAC + 本地哈希 + 服务器逐封核对 + TTY 键数 | 门禁各失败分支表驱动测试 + 端到端测试 |
| 发送门禁 | 白名单全员命中 + dry-run 默认 + TTY SEND + 单发一封 | 本地 TLS SMTP fixture 回归 |
| 内容安全 | 主题/正文/发件人/附件名标 UNTRUSTED；人读面剥离 ANSI/控制符/bidi 覆盖符 | 恶意主题夹具消毒测试 |

**"只读是构造性保证而非文档承诺"**——这是最值得展开的一点：v0.1 的只读不靠自觉，靠四层构造性防线（读接口反射白名单、EXAMINE+PEEK 协议层、命令树白名单、AST 静态审计），任何人想偷偷加一个写命令都会炸测试。写路径被三层锁圈死：go-imap 只准 imapx 包引用、写方法只准出现在 `mutate.go`、这些方法只准策略层调用。

## 5. QQ 方言兼容：真机实测出来的硬骨头

QQ 邮箱的 IMAP 实现有多处不合规。这些不是从文档看来的，是真机实测撞出来并逐条修复的（脱敏实录见 `docs/compat/`），也是这个项目相对通用邮件工具的核心壁垒：

- **畸形 BODYSTRUCTURE / ENVELOPE**：QQ 对部分真实邮件返回不合规的结构响应，会打死严格的 wire 解析器、连累整条会话。对策：批量抓取只信数字和 flag，主题/收发件人/日期全走原始头部字节本地解析——服务器想发畸形都碰不到解析器。
- **HEADER.FIELDS 子集回空壳**：QQ 对字段过滤请求静默返回空，导致退订头/Message-ID 抓取失效（且在合规测试服务器上永远是绿的）。对策：整取完整头部本地挑字段。
- **逐封 EXAMINE 触发限流**：密集的每封一次 SELECT 会被 QQ 随机拒接（失败数漂移是限流指纹）。对策：每文件夹只打开一次、成批 FETCH，往返从数千降到十几。
- **IMAP MOVE 非事务性**：服务器执行成功但客户端没收到回执时会误报失败（方向安全，只少报不多删）。对策：重跑幂等 + 独立只读核对回填真实结果。
- **回收站 30 天自动清理**：QQ 会自动清空"已删除"里超过 30 天的邮件——所以清理的可逆窗口是短的，本地备份才是永久兜底。
- **其他**：AUTHENTICATE 不可用只能走明文 LOGIN、对不支持命令回违规 untagged `* BAD Command!` 会挂死客户端、IDLE 不稳定、改 QQ 密码使全部授权码失效。

对策共性：**面对方言服务器的终极姿态是"只信数字，结构自己解析"**——凡是服务器帮你解析的结构都可能是毒，凡是长度前缀 literal 原样传输的字节都安全。

## 6. Agent 原生协议

机器可读协议是项目最重要的公共接口之一。

- **版本化 JSON 包络**：`{schema_version, command, ok, data, error, warnings, meta}`，字段只增不删。
- **语义化退出码**：0 成功 / 2 用法 / 3 配置 / 10 认证 / 11 服务未开 / 20 网络 / 21 TLS / 30 限流 / 40 不存在 / 50 策略拒绝 / 60 解析失败 / 70 部分成功。Agent 据 `error.retryable` 决定重试还是报人。
- **标准流纪律**：`--json` 下 stdout 永远是合法 JSON，日志/进度/诊断全走 stderr。
- **能力自发现**：`agent-info` 是能力与风险的单一真相源，命令面与风险表由同一份目录驱动、机制上不可能漂移。
- **不可信内容边界**：schema 逐字段标注 `UNTRUSTED`，`agent-info` 给出不可信字段路径清单。

## 7. 技术选型与架构

- **语言 Go**：`CGO_ENABLED=0` 纯静态单文件，一条流水线出 Windows/macOS/Linux 双架构六平台产物，无需用户装任何运行时。选 Go 而非 Rust 的决定性理由：Rust 生态"IMAP 收信"层没有冠军库，而 Go 的 go-imap v2 虽 beta 但活跃、且自带内存 IMAP 服务器可做 CI 集成测试。
- **核心依赖**：go-imap/v2（IMAP，锁 beta 版 + 薄封装隔离）、go-message + enmime 双解析器（MIME，兜底畸形邮件）、zalando/go-keyring（系统凭据）、modernc.org/sqlite（纯 Go SQLite，FTS5）、cobra（CLI）、bluemonday（HTML 清洗）、go-smtp（发送）。
- **模块边界**：第三方协议库全部被业务接口隔离——imapx 是唯一引用 go-imap 的包、mimeparse 唯一引用解析库、secrets 唯一引用 keyring，未来换库不动命令层与 JSON 契约。这条边界由 AST 测试强制执行。
- **发布链**：goreleaser 六平台 + checksums + syft SBOM + GitHub 构建来源 attestation。CI：全平台测试 + vet + golangci-lint + govulncheck + 只读静态审计 + 契约测试，Actions 全部 pin 到 commit SHA。

## 8. 工程亮点（值得展开讲的设计）

1. **只读做成可执行测试而非口头承诺**：反射接口白名单 + AST 四道架构闸跑在 CI 里，架构规则不靠 code review 的记性。
2. **诊断通道是安全设计的对偶**：把错误脱敏做绝的同时必须留一条同样脱敏的 verbose 通道，否则第一个真实 bug 就让你变瞎子。
3. **可续传是长任务的道德底线**：面对 QQ 偶发掐连接，升序分批提交水位让每次中断都能接力而非白跑。
4. **备份门禁的信任锚**：三道门的威胁模型明确"既防文件损坏也防 Agent 走捷径伪造本地备份"，所以门禁的信任锚在服务器真相而非本地文件。
5. **纯 Go 无 cgo 的中文全文检索**：FTS5 默认分词不认中文，自算 bigram 侧列 + 查询路由实现零 C 依赖的中文邮件搜索。
6. **验证式写法**：无 MOVE 能力时的降级路径先双因子确认目标夹副本存在才标删、永不 expunge；确认不了源邮件分毫不动。
7. **"CI 全绿 ≠ 真实可用"**：多个方言 bug 在合规内存服务器上全程绿灯，只有真机才现形——合规 fixture 测正确性，方言 fixture 测生存力，活体探针测真相，三层缺一不可。

## 9. 命令速查

```text
# 配置与诊断
qqmail-cli auth login --email you@qq.com
qqmail-cli doctor --json
qqmail-cli agent-info

# 读
qqmail-cli folder list --json
qqmail-cli envelope list --unread --limit 20 --json
qqmail-cli message show <id> [<id>...] --part text --json
qqmail-cli attachment list <id> --json
qqmail-cli export --all --output ./backup --json

# 理
qqmail-cli sync --json
qqmail-cli search "关键词" --local --json
qqmail-cli triage analyze --json
qqmail-cli triage plan --output plan.json --markdown plan.md [--rules my.toml]
qqmail-cli backup --plan plan.json --output ./backup
qqmail-cli clean --plan plan.json [--execute] [--paranoid]
qqmail-cli restore --plan plan.json [--execute]
qqmail-cli message mark-read <id>... [--execute]
qqmail-cli message move <id>... <folder> [--execute]
qqmail-cli watch --jsonl
qqmail-cli cache inspect | clear [--execute]
qqmail-cli audit list --json

# 发
qqmail-cli send --to you@example.com --subject "主题" --body "正文" [--execute]
qqmail-cli reply <id> --body "..." [--execute]
qqmail-cli forward <id> --to you@example.com [--execute]
```

## 10. 项目状态与边界

- 代码版本 `0.3.0-dev`，覆盖读取内核（v0.1）、本地索引与门禁清理（v0.2）、白名单发送（v0.3）三阶段全部能力面。
- 全量单元与集成测试、静态守卫、漏洞扫描、PowerShell 5.1 冒烟、六平台构建全部通过；真实 QQ 邮箱的读取与清理已实测走通（脱敏观察见 `docs/compat/`）。
- **明确不做**：多服务商（专注 QQ 单点做透，多服务商推荐 himalaya）、实时推送（QQ IDLE 不稳定）、网页版专有功能、群发营销、绕过 QQ 风控、永久删除、内置 AI 模型（AI 通过 Skill/MCP 调用 CLI，不绑定任何模型）。

---

更多：[完整使用手册](USER_GUIDE.md) · [架构说明](ARCHITECTURE.md) · [清理安全模型](v0.2-safety.md) · [安全发送](sending.md) · [兼容性记录](compat/README.md)

维护者：**司徒K** ｜ 公众号：**司徒K** ｜ 个人网站：[www.situking.com](https://www.situking.com)
