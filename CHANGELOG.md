# Changelog

All notable development changes are recorded here. Formal releases remain owner-controlled.

## Unreleased

### Added

- Rerunnable S1-S11 probe entry points with OS-keyring credential loading, redacted ignored results, and dual gates for rate/write observations.
- A combined read-only probe suite that minimizes QQ login churn.
- Redacted 2026-09-01 capability, authentication, search, UID, IDLE, visibility-baseline, and SMTP STARTTLS observations.
- A pure-Go SQLite metadata index with WAL, migrations, single-writer locking, UIDVALIDITY reset handling, and FTS5 Chinese bigram search.
- Incremental `sync` and `search --local` commands with embedded output schemas.
- Deterministic `triage analyze`/`triage plan` commands, bounded TOML extension rules, schema-validated plan files, and sanitized Markdown review output.
- `backup --plan`, which exports and verifies every planned message before recording the backup root in the plan.
- Three-gate `clean`, dry-run-first `message mark-read`/`message move`, polling `watch --jsonl`, cache inspection/whole-file clearing, and dual SQLite/JSONL audit logs.
- Runtime `QQMAILCTL_READONLY` enforcement and agent-info risk levels for read, mutate, and destructive commands.
- Dry-run-first `send`, `reply`, and `forward` with RFC-compliant UTF-8 MIME, allowlisted recipients, real-TTY `SEND` confirmation, content-free audit, and 465 TLS / 587 STARTTLS transport.
- Embedded schemas and a `send` agent risk level for the complete v0.3 command surface.
- v0.3 sending documentation, upgraded Agent skill, and an owner handoff checklist for gated probes and publishing.

### Changed

- Non-ASCII sender/subject filtering now uses the deterministic client window because QQ's synchronizing literal SEARCH continuation was non-compliant in the live observation.
- IMAP sessions send RFC 2971 ID only when the server advertises the capability.
- The development binary version advances from `0.2.0-dev` to `0.3.0-dev` after the full guarded SMTP surface landed.

### Security

- Probe results are excluded from Git and omit credentials, addresses, content, subjects, folder names, and message IDs.
- S4/S5-write/S9/S11 require both an explicit write switch and dedicated-test-account confirmation.
- Message previews and bodies remain absent from the cache unless explicitly enabled; opted-in cache content is documented as unencrypted.
- Human-review Markdown removes control and bidi characters and entity-escapes Markdown syntax from every email-derived field.
- Server mutation calls are confined to one reviewed IMAP boundary and one policy call site. Wire and AST guards reject any EXPUNGE path.
- SMTP execution requires every to/cc/bcc recipient to match a non-empty account allowlist, a real TTY, exact `SEND` confirmation, and readonly disabled; Bcc never enters MIME headers.
- SMTP audit records contain only command/action/result metadata and exclude credentials and message content.
