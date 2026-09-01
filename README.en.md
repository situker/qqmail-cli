# qqmailctl

qqmailctl is an unofficial, safety-first QQ Mail CLI for scripts and AI agents. It reads through standard IMAP, builds a local searchable index, creates verifiable backups, guards server mutations, and sends through recipient-allowlisted SMTP.

> qqmailctl is an independent third-party open-source project. It is not affiliated with, endorsed by, or authorized by Tencent or QQ Mail. It is unrelated to the qmail ecosystem's qmailctl tool.

The current source version is `0.3.0-dev`. As of 2026-09-01, automated tests, vulnerability scanning, the PowerShell 5.1 smoke test, and six Windows/macOS/Linux snapshot builds pass. Formal tags and releases remain owner-controlled.

## Safety defaults

- Authorization codes are stored in the operating-system credential manager and are never accepted as command-line values.
- Read paths use IMAP EXAMINE and BODY.PEEK.
- Email subjects, bodies, senders, quoted text, and attachment names are untrusted data.
- Server mutations and SMTP submission are dry-runs by default and require policy plus real-TTY confirmation.
- `clean` requires verified local backups and server-truth checks; no permanent-delete command exists.
- `send`, `reply`, and `forward` require every recipient to match a non-empty account allowlist.
- Agent sessions should set `QQMAILCTL_READONLY=1`.

## Build and start

Go 1.25 or newer is required to build from source:

```text
git clone https://github.com/situker/qqmailctl.git
cd qqmailctl
go build -o bin/qqmailctl ./cmd/qqmailctl
qqmailctl auth login --email your-account@qq.com
qqmailctl doctor --json
qqmailctl envelope list --unread --limit 20 --json
```

Treat every opaque message ID as indivisible. Batch message reads in one invocation instead of repeatedly logging in.

## Documentation

- [Project introduction](docs/INTRODUCTION.md)
- [Complete user guide](docs/USER_GUIDE.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Safe sending](docs/sending.md)
- [Testing](docs/TESTING.md)
- [Open-source release checklist](docs/RELEASE_CHECKLIST.md)
- [Contributing](CONTRIBUTING.md) and [Security](SECURITY.md)

The detailed documentation is currently Chinese-first. English documentation contributions are welcome if they preserve the project's security semantics and stable JSON contracts.

## Author

**Situ K** ([www.situking.com](https://www.situking.com)) — building safe bridges between AI agents and real-world workflows: skill engineering, agent deployment, and content pipelines.
