# Changelog

All notable development changes are recorded here. Formal releases remain owner-controlled.

## Unreleased

### Added (2026-09-01 pre-publication hardening round)

- `restore --plan`: the regret window for cleaned mail. Locates each planned
  message in the server trash by Message-ID + size from the verified backup
  manifest and moves it back to its original folder; dry-run first, TTY count
  confirmation, readonly-gated, fully audited.
- Cleanup-plan safety scope: `triage plan` now targets only the
  marketing/machine_notification/social_notification categories, the current
  folder, mail older than 30 days at confidence ≥ 0.8 — and never a flagged
  (starred) message, with no override. Exclusions are reported per reason and
  the plan Markdown carries a review warning header. New flags:
  `--include-category`, `--exclude-category`, `--min-confidence`, `--min-age`,
  `--all-folders`.
- `clean --batch-limit` (default 500) caps one execution; re-running a
  partially executed plan now skips already-gone messages with verified
  backups instead of rejecting the whole batch.
- Structured `failure_details` on export results so agents can tell retryable
  failures from permanent ones.

### Fixed (2026-09-02, first real-mailbox cleanup run)

- The clean gate batches server-truth checks per folder instead of per
  message: a 2,823-message plan previously issued ~8,500 round trips (one
  EXAMINE + one envelope fetch + one header fetch each), which tripped QQ's
  connection-rate limiting and failed a large, drifting fraction of messages
  with "folder examination failed". The gate now examines each folder once
  and batch-fetches envelopes/headers in 500-UID chunks (~15 round trips).
- A missing Message-ID is no longer treated as a failed identity comparison:
  wild email legitimately lacks the header, so when the verified backup has
  no Message-ID the message is gated on UIDVALIDITY+UID+RFC822.SIZE — but a
  one-sided presence still rejects.
- A refused clean batch now prints its failure reasons grouped on stderr, and
  the gate emits a progress heartbeat every 250 messages (previously the
  verification phase was silent for many minutes and a whole-batch refusal
  came with no explanation).

### Fixed (2026-09-02, first real-mailbox triage run)

- Bulk fetches no longer request server-parsed structure at all: a live QQ
  mailbox contains messages whose malformed `BODYSTRUCTURE` **and** malformed
  `ENVELOPE` responses each desync the go-imap wire parser and kill the whole
  session, making `sync` permanently fail (see docs/compat/qq-20260902.md).
  The server is now trusted only for numbers and flags; subject, addresses,
  and date arrive as raw header literals and are parsed locally with
  charset-aware lenient parsing (`ParseAddressListLenient`). A source-level
  guard test keeps ENVELOPE/BODYSTRUCTURE out of bulk fetches;
  `has_attachments` in bulk listings is documented as always false.
- Header fetches request the full `BODY.PEEK[HEADER]` instead of
  HEADER.FIELDS subsets: a live probe showed QQ answers HEADER.FIELDS with an
  empty two-byte block while returning the complete header normally — every
  header-fields fetch (Message-ID, List-Unsubscribe, Precedence) had been
  silently empty against the real server while passing against the compliant
  CI server. Fields are picked locally; a source-level guard bans
  HEADER.FIELDS subsets from the client.
- `--verbose` now prints the redacted underlying cause chain to stderr on
  command failure; previously an `internal` error was undebuggable by design.

### Fixed (2026-09-01 pre-publication hardening round)

- Every cobra usage failure now returns the JSON contract (usage/exit 2):
  unknown subcommands no longer print help to stdout with exit 0, a bare
  `qqmailctl login` is no longer misclassified as an authentication failure,
  and `meta.duration_ms` can no longer be garbage.
- Watch/sync no longer re-announce the newest message on every poll (RFC 3501
  `N:*` always matches the highest UID; the window filter now drops it).
- A plain `sync` no longer wipes cached Chinese body bigrams for the recent
  flag-refresh window.
- `auth login` re-login preserves the account's send allowlist and host
  overrides instead of overwriting the entry.
- Expunge guards now also ban `UIDExpunge` and `UnselectAndExpunge` (CLOSE)
  and scan the whole imapx tree; the readonly blocking matrix asserts the
  rejection actually came from the readonly gate.
- `QQMAILCTL_READONLY` fails closed on unrecognized values; `doctor` reports
  the live readonly state; text-mode partial results exit 70.
- Sync fetches in ascending 500-message batches with per-batch watermark
  commits, so a huge first sync survives timeouts and resumes.
- Single-line human output flattens newlines from decoded subjects
  (`SanitizeLine`), closing a fake-table-row spoof in list views and TTY
  confirmation summaries.
- `scripts/check-docs.ps1` gained a UTF-8 BOM and explicit UTF-8 reads; the
  disclaimer and relative-time gates were previously passing only by a
  double-mojibake coincidence under Windows PowerShell 5.1.
- GitHub Actions are pinned to commit SHAs and govulncheck to a fixed
  version; git identity for this repository is the GitHub noreply address.

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
- Public-facing project introduction, architecture, complete user guide, dated test plan and open-source release checklist.
- Pull-request and feature-request templates plus expanded contribution and security guidance.

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
