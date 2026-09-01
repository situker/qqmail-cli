## 问题与方案

说明这个 PR 解决的问题、采用的方案和明确不做的范围。

## 安全与契约影响

- [ ] 不包含授权码、真实邮箱地址、邮件正文、附件、消息 ID 或未脱敏日志
- [ ] 没有新增裸 EXPUNGE、永久删除、确认绕过或隐式发送路径
- [ ] 读路径仍使用 EXAMINE/BODY.PEEK
- [ ] 新写路径经过 readonly、policy、TTY 确认和双审计
- [ ] JSON/schema/退出码保持向后兼容，或已清楚说明兼容性影响
- [ ] 新邮件派生字段已标记并按 untrusted 处理

不适用的项目请说明原因，不要机械勾选。

## 验证

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `golangci-lint run`
- [ ] `govulncheck ./...`
- [ ] 相关 schema/readonly/方言/脱敏测试已增加或更新
- [ ] Windows PowerShell smoke（影响 CLI/输出时）

## 文档

列出更新的 README/docs/SKILL；若无需更新，说明原因。
