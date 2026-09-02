<div align="center">

# qqmail-cli

**The QQ Mail CLI that's actually safe to hand to your scripts and AI agents.**

[![CI](https://github.com/situker/qqmail-cli/actions/workflows/ci.yaml/badge.svg)](https://github.com/situker/qqmail-cli/actions/workflows/ci.yaml)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

[简体中文](README.md) · **English**

</div>

> qqmail-cli is an independent third-party open-source project. It is not affiliated with, endorsed by, or authorized by Tencent or QQ Mail. It works exclusively through the standard IMAP/SMTP services that users enable themselves, and it is unrelated to the qmail ecosystem's qmailctl tool.

qqmail-cli is a safety-first command-line client for QQ Mail: reading, searching, rule-based triage, verifiable backups, gated cleanup, and allowlisted sending — all through a stable, versioned JSON contract. It does not solve "can I connect to the mailbox"; it solves everything that comes after: whether credentials leak, whether an agent can destroy mail by accident, and whether a hostile email can turn around and steer the agent.

## About the author

**司徒K (Situ K)** — a serial entrepreneur and long-term thinker.

- WeChat Official Account: 司徒K
- Website: [www.situking.com](https://www.situking.com)

If this project is useful to you, a star is appreciated. To talk AI deployment, skill/agent engineering, or the design trade-offs behind this tool, the WeChat account and the website are both good ways to reach me.

```console
$ qqmail-cli envelope list --unread --limit 2 --json
{
  "schema_version": "1",
  "command": "envelope.list",
  "ok": true,
  "data": {
    "envelopes": [
      {
        "id": "m1_eyJmIjoiSU5CT1giLCJ2IjoxNDI1LCJ1Ijo4MzQ3fQ",
        "uid": 8347,
        "uidvalidity": 1425,
        "folder": "INBOX",
        "subject": "September statement",
        "from": [{ "email": "billing@example.com" }],
        "to": [{ "email": "me@qq.com" }],
        "date": "2026-08-30T09:12:00+08:00",
        "internal_date": "2026-08-30T09:12:00+08:00",
        "size_bytes": 18204,
        "flags": [],
        "has_attachments": false
      },
      { "…": "second envelope elided; identical shape" }
    ],
    "page": { "next_before_uid": 8346 }
  },
  "error": null,
  "warnings": [],
  "meta": { "account": "personal", "duration_ms": 1, "truncated": false, "search_mode": "server" }
}

$ qqmail-cli message show "m1_eyJmIjoiSU5CT1giLCJ2IjoxNDI1LCJ1Ijo4MzQ3fQ" \
                        "m1_eyJmIjoiSU5CT1giLCJ2IjoxNDI1LCJ1Ijo4MzQ2fQ" --json
```

(The sample is generated from a real run and validates against the project's own schema; live output is compact single-line JSON, pretty-printed here for reading.) List envelopes once, then batch-read every message in a single invocation — ids are opaque tokens, copied verbatim. All reads go through `EXAMINE` + `BODY.PEEK`; the unread state on the server is never touched.

## Documentation quick links

[Five-minute start](#five-minute-start) · [Complete user guide](docs/USER_GUIDE.md) · [Agent discipline](#for-ai-agents) · [Security model](#security-model) · [Architecture](docs/ARCHITECTURE.md) · [FAQ](#faq)

## Why qqmail-cli

Plenty of libraries can send and receive mail. Few treat the following as first-class concerns:

- **Credentials never touch plaintext** — the 16-character authorization code lives only in the OS credential store (Windows Credential Manager / macOS Keychain / Linux Secret Service). No command-line flag accepts the authorization code, it never appears in config files, and logs, error output, and even panic traces are redacted.
- **Reads leave no trace** — the read path is hard-wired to `EXAMINE` + `BODY.PEEK`; looking at a message never marks it read.
- **Email is data, not instructions** — subjects, bodies, senders, and attachment names are untrusted input: JSON fields carry `UNTRUSTED` annotations, and every human-facing surface strips ANSI escapes, control characters, and bidi overrides. A crafted email cannot forge a single line of your confirmation screen.
- **Writes are gated** — every server mutation is a dry-run by default; real execution must clear the policy layer and a typed confirmation on an interactive terminal, and every action leaves a full audit trail. **No bypass flag exists** — and a test enforces that no bypass flag exists.
- **Deletion has an undo** — cleanup requires a hash-verified, HMAC-signed local backup first, then per-message server-truth checks at execution time. Starred mail is protected — no override exists; messages only ever move to the trash folder (there is no permanent-delete command), and protocol guards ban even the code paths that could emit `EXPUNGE`/`CLOSE`. Regrets are handled by `restore`, which walks the same plan back out of the trash.
- **Chinese email done right** — GB2312/GBK/GB18030 subjects and attachment names, Modified UTF-7 folder names, a dual-parser fallback for malformed MIME, and FTS5 character-bigram full-text search, all pinned by a synthetic regression corpus.
- **Agent-native** — a versioned JSON envelope, `agent-info` capability discovery, semantic exit codes (machine-decidable retryability), a bundled agent skill file, and `QQMAIL_CLI_READONLY=1` to lock down the entire write surface with one environment variable.
- **Local-first, zero telemetry** — the only network connection is the TLS session between your machine and QQ's servers. The local index stores no bodies unless you opt in; `cache clear` removes the whole database file.

None of this lives only in the documentation: the read-only boundary, the write-path confinement, the no-bypass rule, and the EXPUNGE ban are all enforced by guard tests that run in CI (interface reflection allowlists, `go/ast` static scans, and wire-level protocol assertions).

## When not to use it

An honest list of non-goals saves you more time than a feature list:

- **Your mailbox is not QQ/Foxmail.** This project deliberately goes deep on one provider. For multi-provider needs, use [himalaya](https://github.com/pimalaya/himalaya).
- **You need real-time push.** Timestamped live-server observations show QQ's IMAP IDLE notifications are unreliable, so `watch` is honest polling (60-second default) and promises nothing it cannot keep.
- **You need web-only features.** Contacts, calendar, and large-file transfer are not part of standard IMAP/SMTP, and this project will never reverse-engineer the web interface.
- **You want bulk marketing email.** Sending is built around one message per invocation, a recipient allowlist, and a human confirmation — by design.
- **You want to evade QQ's rate limits.** The design goes the other way: batched verbs to reduce logins, explicit rate-limit exits with wait guidance, and zero adversarial behavior.

## Installation

### Option 1: prebuilt binaries

Once public releases are available, download the archive for your platform from [GitHub Releases](https://github.com/situker/qqmail-cli/releases):

| Platform | Architectures | Notes |
|---|---|---|
| Windows | amd64 / arm64 | Unzip `qqmail-cli.exe` and put it on your PATH |
| macOS | Intel / Apple Silicon | `chmod +x` before first use |
| Linux | amd64 / arm64 | Same as above |

Verify the checksum first (`checksums.txt` ships with every release, together with build-provenance attestations verifiable via `gh attestation verify`):

```powershell
Get-FileHash .\qqmail-cli.exe -Algorithm SHA256
```

On Windows, SmartScreen will warn about an unsigned publisher on first run. That is expected: verify the checksum, then choose "More info → Run anyway".

### Option 2: build from source

Requires Go 1.25+. No CGO, no external toolchain:

```bash
git clone https://github.com/situker/qqmail-cli.git
cd qqmail-cli
CGO_ENABLED=0 go build -o bin/qqmail-cli ./cmd/qqmail-cli
```

Windows PowerShell:

```powershell
git clone https://github.com/situker/qqmail-cli.git
cd qqmail-cli
$env:CGO_ENABLED = "0"
go build -o .\bin\qqmail-cli.exe .\cmd\qqmail-cli
.\bin\qqmail-cli.exe version --json
```

### Windows PowerShell 5.1 and Chinese text

Before piping JSON containing Chinese text through PowerShell 5.1:

```powershell
$utf8 = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8
```

## Five-minute start

```text
1. QQ Mail web → Settings → Account & Security → enable IMAP/SMTP and generate
   the 16-character authorization code
   (official guide: https://service.mail.qq.com/detail/0/1087)

2. qqmail-cli auth login --email your-account@qq.com
   The code is read with hidden input and stored in the OS credential manager
   only after the connection is verified

3. qqmail-cli doctor --json
   One command diagnoses configuration, credentials, TLS, login, and server
   capabilities

4. qqmail-cli envelope list --unread --limit 20 --json

5. qqmail-cli message show <id> --json
   Copy the opaque id verbatim from the previous output; never construct one
```

The authorization code is **not** your QQ password — the number-one newcomer trap, and `doctor` diagnoses it. Changing your QQ password immediately invalidates every authorization code; just run `auth login` again (existing configuration survives).

## Common workflows

### Local search and mailbox cleanup

```text
qqmail-cli sync --json                                # incremental metadata sync into SQLite
qqmail-cli search "invoice" --local --json            # FTS5 full-text search (Chinese-capable)
qqmail-cli triage analyze --json                      # rule buckets: marketing / notifications / …
qqmail-cli triage plan --output plan.json --markdown plan.md
qqmail-cli backup --plan plan.json --output backup    # full .eml backup of every planned message
qqmail-cli clean --plan plan.json                     # dry-run: reports, touches nothing
qqmail-cli clean --plan plan.json --execute           # three gates + TTY confirmation → trash
qqmail-cli restore --plan plan.json                   # the regret window: walk it back (dry-run)
```

Triage is deterministic and local — the CLI never calls an AI model. A plan defaults to a conservative scope: only unambiguously junk categories (`marketing`, `machine_notification`, `social_notification`), only the inbox, only mail older than 30 days, and never a starred message — with every exclusion counted and reported for your review. Execution defaults to a 500-message batch cap (raiseable only via an explicit `--batch-limit`), and every single message must pass local-backup verification plus per-message server-truth checks.

### Safe sending

Configure a recipient allowlist first (an empty allowlist rejects everything):

```toml
[accounts.personal]
email = "your-account@qq.com"
send_allowlist = ["you@example.com", "*@your-company.example"]
```

```text
qqmail-cli send --to you@example.com --subject "Subject" --body "Body"   # dry-run: full envelope preview
qqmail-cli reply <id> --body "Reply text"                                # correct threading headers
qqmail-cli forward <id> --to you@example.com --body "FYI"
```

Real submission requires `--execute`, every to/cc/bcc recipient matching the allowlist, and a human typing `SEND` on a real terminal. One message per invocation, always.

## For AI agents

The first line of every agent session:

```powershell
$env:QQMAIL_CLI_READONLY = "1"    # locks all mutations and sending; unknown values fail closed
```

The agent integration kit:

- `qqmail-cli agent-info` — a machine-readable capability manifest: every command with its risk level (read/mutate/destructive/send), the untrusted field paths, and the live read-only state.
- `qqmail-cli schema <command>` — the embedded JSON Schema, so agents can self-validate before consuming any new shape.
- [`skills/qqmail-cli/SKILL.md`](skills/qqmail-cli/SKILL.md) — a bundled agent skill file. It ships with the CLI, so installing the binary also installs the full calling discipline (directly usable from Claude Code and similar harnesses).

The key discipline: run `envelope list` once, then hand every id to **one** `message show` batch call — each CLI invocation is one IMAP login, and frequent logins trigger QQ's rate controls. Exit codes are semantic: retry only when `error.retryable` is true (exponential backoff, two attempts max); on exit 30 (rate-limited) stop and wait 10–15 minutes; exit 50 (policy denied) means a safety gate is doing its job and the answer is a human, not a retry.

## Security model

| Layer | Mechanism | Proof |
|---|---|---|
| Credentials | OS credential store; env injection needs an explicit `--auth-code-env`; all channels redacted | Fixture-secret scans across every output channel, panic path included |
| Read-only | EXAMINE + PEEK; allowlisted reader interface | Interface-reflection test + in-memory IMAP server behavior assertions |
| Write boundary | go-imap write methods confined to one file, callable only by the policy layer | `go/ast` static scans in CI |
| No deletion | No permanent-delete command; EXPUNGE/CLOSE banned everywhere | AST ban table + wire-transcript assertions |
| Cleanup gates | Backup HMAC + local hashes + per-message server truth + typed TTY count | Table-driven gate-failure tests + end-to-end test |
| Send gates | Full allowlist match + dry-run default + typed TTY `SEND` + one message per run | Local TLS SMTP fixture regressions |

Further reading: [Cleanup safety model](docs/v0.2-safety.md) · [Safe sending](docs/sending.md) · [Security policy](SECURITY.md)

## FAQ

<details>
<summary><b>Is the authorization code my QQ password?</b></summary>

No. It is a separate 16-character credential that QQ Mail generates for third-party clients after you enable IMAP/SMTP under Settings → Account & Security. qqmail-cli only ever accepts authorization codes. Note: changing your QQ password immediately invalidates all authorization codes.
</details>

<details>
<summary><b>Does my mail get uploaded anywhere?</b></summary>

No. qqmail-cli is zero-telemetry and local-first: the only network connection is the direct TLS session between your machine and QQ's servers. The local index stores envelope metadata only; persisting bodies requires an explicit `--cache-bodies`, and `cache clear` deletes the entire database file.
</details>

<details>
<summary><b>Can an agent destroy my mail by accident?</b></summary>

This question is the project's founding design constraint. Five layers of defense: `QQMAIL_CLI_READONLY=1` locks the write surface at the source; cleanup plans default to unambiguously junk categories with starred mail unconditionally excluded; execution demands verified backups plus per-message server checks; the confirmation requires a human typing an exact count on a real terminal, which an agent's pipe cannot satisfy; and even a fully confirmed cleanup only moves mail to the trash — `restore` walks it back, and local `.eml` backups remain. A permanent-delete command does not exist in this codebase.
</details>

<details>
<summary><b>Why is there no real-time push?</b></summary>

Timestamped live-server observations show QQ's IMAP IDLE notifications are unreliable (see the <a href="docs/compat/README.md">compatibility records</a>). Rather than make a real-time promise it cannot keep, <code>watch --jsonl</code> polls honestly, with UID-watermark deduplication so events are never re-announced.
</details>

<details>
<summary><b>Will this trip QQ's rate limits?</b></summary>

Tencent officially confirms login-frequency and connection limits with undisclosed thresholds. qqmail-cli cooperates rather than fights them: one connection per invocation, batched verbs to reduce logins, an explicit exit code 30 with wait guidance when limited, and no automatic retry on authentication failures. The bundled <code>SKILL.md</code> teaches agents the same discipline.
</details>

<details>
<summary><b>Windows says "unknown publisher"?</b></summary>

The binaries are not yet code-signed, so the SmartScreen prompt is expected. Verify the hash against <code>checksums.txt</code> (releases also carry build-provenance attestations), then choose "More info → Run anyway".
</details>

<details>
<summary><b>What about 163, Gmail, or corporate mailboxes?</b></summary>

Not supported, by design. qqmail-cli's entire value proposition is doing one provider properly: official documentation verified line by line, server dialects tested against reality, Chinese-language handling covered by regression tests. For multi-provider needs, <a href="https://github.com/pimalaya/himalaya">himalaya</a> is the right tool.
</details>

## Related projects

- [himalaya](https://github.com/pimalaya/himalaya) — a mature multi-backend email CLI in the Rust ecosystem and the reference point for noun-verb email command trees (this project aligns with its syntax). Choose it for multi-provider setups.
- Generic IMAP libraries solve "connecting"; qqmail-cli solves "trusting the connection to automation". The two are complements, not competitors.

## Project status

Source version `0.3.0-dev`, covering the full three-stage surface: the read-only core (v0.1), local indexing with gated cleanup (v0.2), and allowlisted sending (v0.3). As of 2026-09-01, the complete unit and integration suites, static guards, vulnerability scans, the PowerShell 5.1 smoke test, and six-platform snapshot builds all pass; timestamped read-only observations against real QQ servers live in the [compatibility records](docs/compat/README.md). Formal tags and public releases will follow on the maintainer's schedule; see the [CHANGELOG](CHANGELOG.md).

## Contributing

```text
go test ./...          # full suite, including read-only guards and contract tests
go vet ./...
golangci-lint run
govulncheck ./...
```

The JSON output is a stable contract (`schema_version: "1"`, fields are add-only); human-readable text is not. Read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting; report security issues privately per [SECURITY.md](SECURITY.md) rather than opening a public issue. Test fixtures must be synthetic — the repository accepts no real email samples.

Detailed documentation is Chinese-first for the time being; English documentation contributions are very welcome as long as they preserve the security semantics and the stable JSON contracts.

## License

[Apache License 2.0](LICENSE) · copyright and author attribution in [NOTICE](NOTICE) (must be retained in redistributions)
