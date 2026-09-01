# qqmailctl implementation status

Updated: 2026-09-01 (post-review hardening round)

This repository implements the v0.1, v0.2 and v0.3 development surfaces requested from `TECHNICAL_PLAN.md`. The binary reports `0.3.0-dev`; it has not been tagged or published.

## 2026-09-01 pre-publication hardening round

A six-track independent review (security red-lines, leak scan, clean-loop
logic, contract conformance, code quality, test authenticity) was applied in
full. Every blocker/major and the actionable minors were fixed:

- Anti-accidental-deletion package: cleanup-category whitelist with folder,
  age, confidence and no-override flagged-mail protection; `clean
  --batch-limit`; already-gone re-run recovery; the new `restore --plan`
  command as a regret window; documentation for trash auto-purge and
  interrupted-clean recovery.
- Contract: cobra usage failures now emit the JSON envelope with usage/exit 2
  (previously help text on stdout with exit 0); `duration_ms` sanity; honest
  `doctor` readonly; structured export failure details; pagination cursor
  advances past examined windows; `--folder`-vs-id authority enforced on every
  id-accepting command; untrusted_paths is superset-tested against schema
  UNTRUSTED annotations.
- Correctness: RFC 3501 `N:*` watermark echo filtered centrally (watch/sync
  duplicates); FTS bigram preservation across plain syncs; `auth login`
  re-login no longer wipes account configuration; ascending batched sync with
  per-batch watermark commits.
- Guards: `UIDExpunge`/`UnselectAndExpunge` (CLOSE) banned everywhere and the
  wire guard rejects CLOSE; imapx scan recursive; readonly env fails closed;
  the readonly blocking matrix now asserts the readonly gate itself rejected
  (a sibling guard can no longer satisfy it) and covers `restore`.
- Tests: clean --execute end-to-end, confirmation-gate three-state tests,
  SMTP 465→587 fallback semantics on a local TLS fixture, cleaner gate
  failure-branch table, v0.1 read-command contract suite plus manifest schema
  validation, MIME per-variant structure assertions, allowlist partial-hit and
  Bcc coverage, watch reset/echo tests.
- Hygiene: git identity switched to the GitHub noreply address (history
  rewrite before first push), Actions pinned to commit SHAs, govulncheck
  pinned, check-docs.ps1 encoding fixed (its gates previously passed by a
  double-mojibake coincidence), dead code removed, sanitize policy built once,
  author sections added to both READMEs.

## Implemented and locally verified

- M0: standalone Go repository, Apache-2.0, version/completion, stable JSON envelope, semantic exit codes, redaction, schemas, three-platform CI.
- M1: TOML multi-account configuration, OS keyring, explicit env fallback, hidden/stdin login entry, login-before-save, status/logout/list/use.
- M2: strict TLS, LOGIN, capability checks before and after authentication, command watchdog plus socket deadlines, folder list, EXAMINE, UID search/fetch, BODY.PEEK, stable IDs and UIDVALIDITY rejection. The in-memory TLS integration test proves PEEK does not mark a message read; the dialect simulator proves an untagged `* BAD Command!` cannot hang login.
- M3: go-message primary parser, enmime fallback, Chinese charset wiring, sanitized HTML, text fallback, attachment metadata, a 64 MiB message boundary, and a 40-case synthetic MIME/charset corpus.
- M4: folder/envelope/message/attachment commands, UID pagination, exact post-filtering for `--since`, server/client-window search paths, batch message reads and one-based attachment indices.
- M5: doctor checks configuration, credential source, strict TLS/login, pre/post-login capabilities, custom-server warning and Windows UTF-8 guidance. Full QQ-specific error-string mapping remains dependent on M8 evidence.
- M6: `.eml` export, safe paths, SHA-256, account HMAC key in keyring, single-manifest merge, idempotent resume, offline verification and tamper tests; manifest/plan schemas are embedded.
- M7: `agent-info`, `schema`, valid distributable `skills/qqmailctl/SKILL.md`, bilingual top-level guidance, complete introduction/user/architecture/testing/release docs, and public contribution/security templates.
- M9 preparation: GoReleaser v2 config validates and cross-builds six Windows/macOS/Linux targets; GitHub release workflow includes checksums, Syft SBOMs and build provenance attestation. No tag, package-name reservation, or remote publication has been performed.

## Verification completed

- `go test ./...`
- `go vet ./...`
- `golangci-lint run ./...` — 0 issues
- `govulncheck ./...` — no vulnerabilities found after upgrading `x/net` to v0.56.0 and `x/text` to v0.39.0
- `actionlint` — workflows valid
- `scripts/smoke-ps51.ps1` — passed
- `goreleaser check` and `goreleaser build --snapshot --clean` — six targets passed
- Skill Creator `quick_validate.py` — `Skill is valid!`
- Owner-operated live QQ Mail smoke on Windows (2026-09-01) — authorization verified and saved, unread envelope listing and message reading succeeded, and both a second CLI query and the QQ web UI confirmed that `message show` preserved the unread state. The redacted observation is in `docs/compat/qq-20260901.md`.
- Read-only S1/S2/S3/S5/S6/S7/S10 observations (2026-09-01) — capability/authentication/search/UIDVALIDITY/short-IDLE/visibility-baseline/STARTTLS evidence is recorded in `docs/compat/qq-20260901-readonly-spikes.md`. All raw mailbox counts and hashes remain in Git-ignored local output.

Final `0.3.0-dev` verification pass on 2026-09-01:

- `go test ./...` — every package passed, including schema contracts, readonly/static mutation guards, no-bare-EXPUNGE wire assertions, MIME/threading/allowlist tests, audit-content exclusion and SMTP error mapping.
- `go vet ./...` — passed; `golangci-lint run` — 0 issues; `govulncheck ./...` — no vulnerabilities found.
- `CGO_ENABLED=0 go build -o bin/qqmailctl.exe ./cmd/qqmailctl` — passed; `version --json` reports `0.3.0-dev`; `scripts/smoke-ps51.ps1` passed with `read`, `mutate`, `destructive` and `send` risks present.
- Skill Creator `quick_validate.py` — `Skill is valid!` after the v0.3 Skill rewrite.
- `goreleaser check` — configuration valid; final `goreleaser build --snapshot --clean` produced exactly six binaries: Windows, macOS and Linux on amd64 and arm64.
- No real SMTP submission or real-account server mutation was part of this pass.

## v0.2 implemented

- SQLite index foundation: `modernc.org/sqlite` v1.57.0, numbered `PRAGMA user_version` migration, WAL, 5-second busy timeout, OS-level single-writer file lock, external-content FTS5, per-folder UID watermarks, UIDVALIDITY reset semantics, and audit storage.
- `sync`: all selectable folders are read with EXAMINE/PEEK semantics; new UIDs are fetched above `last_seen_uid`, the recent 200-message window refreshes flags, and classification headers are fetched without downloading bodies. `--cache-previews` and `--cache-bodies` are explicit opt-ins; both fields remain SQL NULL by default.
- `search <query> --local`: FTS5 local search with an application-generated character-bigram shadow column for Chinese queries.
- Contract schemas and command-level schema tests are present for `sync` and `search`.
- `triage analyze` and `triage plan`: deterministic built-in classification, domain/age/size/unread-rate buckets, bounded user TOML regular-expression rules, embedded plan-schema validation, and a sender-grouped Markdown review whose untrusted fields are control/bidi stripped and Markdown entity-escaped.
- `backup --plan`: reuses the `.eml` export/HMAC engine, merges into one manifest, verifies the completed local backup, and only then records the absolute `backup_root` in the schema-valid plan. Manifest entries now add the optional `message_id` server-truth comparison key.
- `clean --plan`: dry-run by default; execute requires manifest HMAC and local hash verification, per-message UIDVALIDITY/RFC822.SIZE/Message-ID server checks, optional paranoid full-body SHA-256, and exact-count confirmation on a real TTY. A single failure rejects the whole queue before mutation.
- Mutation boundary: go-imap write calls exist only in `internal/imapx/mutate.go`, are callable only by `internal/policy`, and have AST and wire-transcript guards. MOVE is used only when advertised. The S5-pending fallback is COPY + destination confirmation + STORE `\\Deleted`, never any form of expunge; its report marks the source as retained.
- `message mark-read` and `message move`: dry-run defaults, execute/TTY gates, centralized policy, SQLite audit plus append-only JSONL.
- `watch --jsonl`: 60-second polling by default, UID watermark deltas and UIDVALIDITY reset events; IDLE is absent from the product path.
- `cache inspect`, dry-run-first `cache clear --execute`, and `audit list`. Cache clear removes the whole DB/WAL/SHM set; the content-free audit JSONL remains independently readable and records the clear itself.
- `QQMAILCTL_READONLY=1` blocks every declared mutate/destructive command before credential access or network dialing. `agent-info` reports the live readonly state and read/mutate/destructive risk levels.
- All v0.2 commands have embedded data/event schemas and command-level contract tests. The intermediate `0.2.0-dev` milestone was independently green before v0.3 work began.

## v0.3 implemented

- `send`: `go-smtp` v0.25.0 with QQ-default 465 implicit TLS and connection-failure fallback to 587 STARTTLS, TLS 1.2 minimum, PLAIN authorization-code authentication, bounded timeouts and SMTP error classification. Rate-limit responses map to `rate_limited` / exit 30.
- MIME construction: RFC 2047 Chinese subject/display-name encoding, MIME parameter encoding for Chinese attachment names, quoted-printable UTF-8 bodies, base64 attachments, Bcc omitted from MIME headers, header-injection guards, 1 MiB body-file limit and 20 MiB combined attachment limit.
- Sending policy: dry-run by default with a sanitized from/to/cc/bcc/subject/body/attachment summary; execution requires a non-empty account allowlist matching every recipient, a real TTY, exact `SEND` confirmation and `QQMAILCTL_READONLY` disabled. One invocation submits at most one message and exposes no bypass flag.
- `reply` and `forward`: BODY.PEEK source reads, `Reply-To` preference, correct `Re:`/`Fwd:` prefixing, reply `In-Reply-To` plus cumulative `References`, quoted original text and forwarded attachments. They use every `send` gate.
- Audit: every actual SMTP attempt/result is recorded in SQLite and the content-free JSONL; credentials, recipients, subjects, bodies and attachment names are excluded.
- `agent-info` reports `send` risk for all three commands and lists every new untrusted summary path. Each command has an embedded schema and command-level contract validation.
- The repository Skill now covers v0.2/v0.3 commands, the complete exit decision table, default Agent readonly discipline, and mandatory human presence for `clean` and SMTP execution.
- MCP was deliberately not implemented.

### Deviations and conservative decisions

- The dedicated-account S5 write probe has not been authorized, so the fallback deliberately stops after COPY confirmation and per-UID STORE `\\Deleted`; it does not use UID EXPUNGE even when UIDPLUS is advertised. This is the conservative branch required by the plan.
- `cache clear` adds an `--execute` plus TTY `CLEAR` confirmation beyond the reserved command sketch. This is additive and does not alter existing contract shapes.
- No real-account mutation was run during implementation. Only the dual-gated probe can authorize such a test.
- The command surface composes exactly one SMTP envelope per process, so the requested per-invocation cap is one. A 2-second interval constant is reserved for any future in-process batching; it is not represented as a cross-process anti-abuse guarantee.
- The SMTP fallback is intentionally limited to 465 connection/TLS setup failure. Authentication or submission rejection on a working 465 connection is returned as-is and is never retried on 587, preventing accidental duplicate delivery.
- No real SMTP message was sent during implementation; all execution-path tests use an injected in-memory transport.

## Externally blocked / deliberately not performed

- S4, the S5 write phase, the second S7 web-option snapshot, S8, S9 and S11 remain gated/manual. The read-only observations above do not replace dedicated-account write/rate-limit evidence.
- macOS Keychain and a headless Linux Secret Service failure path still need platform CI/real-host confirmation.
- Remote GitHub repository creation, push, tag, package-name reservation and publication require the owner's accounts/authorization and were not attempted. Exact handoff steps are in `docs/OWNER_CHECKLIST.md`.
- The sampled metadata-only QQ UID FETCH response was well formed; the historical malformed full-FETCH variant remains a pending fixture until an exact redacted shape is captured. The implemented timeout guard prevents indefinite blocking.
