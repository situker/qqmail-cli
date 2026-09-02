# v0.3 安全发送

`send`、`reply`、`forward` 共用同一套策略层、白名单、TTY 确认、审计和 SMTP 传输。CLI 本体不调用任何 AI 模型。

## 配置白名单

授权码仍只保存在系统凭据管理器。`auth status --json` 的 `config_path` 会显示当前配置文件位置；发送配置只包含服务器地址和允许的收件人：

```toml
[accounts.personal]
email = "your-account@qq.com"
smtp_host = "smtp.qq.com"
smtp_port = 465
send_allowlist = ["you@example.com", "*@your-company.example"]
```

省略 SMTP 地址时默认使用 `smtp.qq.com:465`。白名单条目只支持精确邮箱或 `*@domain`；`to`、`cc`、`bcc` 中任何一个地址未命中都会整封拒发，空白名单也拒发。

## 先 dry-run，再由人在场执行

```text
qqmail-cli send --to you@example.com --cc teammate@your-company.example --subject "主题" --body-file body.txt --attach report.pdf
qqmail-cli reply <opaque-id> --body "回复内容"
qqmail-cli forward <opaque-id> --to you@example.com --body "转发说明"
```

默认 dry-run 会构建 MIME、检查资源上限并展示 from/to/cc/bcc、主题、正文摘要和附件清单，但不会连接 SMTP。`reply` 和 `forward` 会用 BODY.PEEK 读取原邮件；生成的主题、引用体、原发件人与附件名仍是不可信数据，人读预览会剥控制符、ANSI 和 bidi 覆盖符。

真正发送必须同时满足：

1. 未启用 `QQMAIL_CLI_READONLY=1`。
2. 全部收件人命中非空白名单。
3. stdin 是真实 TTY。
4. 人工核对摘要并键入 `SEND`。
5. 系统凭据管理器中存在该账号授权码。

执行示例是在人工终端中给已审阅的 dry-run 命令追加 `--execute`。项目不提供 `--yes`、`--force`、`--no-confirm` 或其他 bypass flag。

每次 CLI 调用只提交一封邮件，最多 10 个收件人，附件合计最多 20 MiB，正文文件最多 1 MiB。内部为未来批处理保留的封间隔常量为 2 秒；当前单封命令不构成跨进程频率保证。若 SMTP 返回限流，命令映射为 `rate_limited` / 退出码 30，应停止重试并等待 10–15 分钟。

## MIME 与传输

- 中文主题和显示名使用 RFC 2047 编码。
- 中文附件名由标准 MIME 参数编码处理（RFC 2231/兼容形式）。
- Bcc 只进入 SMTP envelope，不写入 MIME 头。
- `reply` 设置 `In-Reply-To` 和累积 `References`；`forward` 保留引用链但不伪装成直接回复。
- QQ 默认优先 465 隐式 TLS；只有连接建立失败才尝试 587 STARTTLS。TLS 最低版本为 1.2。
- 所有发送尝试与结果写 SQLite audit 表及内容无关的 JSONL；审计不记录授权码、地址、主题、正文或附件名。

S9 真实发送配额探针尚未运行。只有专用测试邮箱、双重写门（`QQMAIL_CLI_E2E_WRITE=1` + `QQMAIL_CLI_DEDICATED_TEST_ACCOUNT=1`）和自发自收件人条件同时满足时才能触发。
