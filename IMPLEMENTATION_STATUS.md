# qqmailctl implementation status

Updated: 2026-09-01

This repository implements the v0.1 read-only product surface from `TECHNICAL_PLAN.md`. It has not been tagged or published.

v0.2 and v0.3 development is in progress. Completed slices remain independently buildable and are committed only after their quality gates pass.

## Implemented and locally verified

- M0: standalone Go repository, Apache-2.0, version/completion, stable JSON envelope, semantic exit codes, redaction, schemas, three-platform CI.
- M1: TOML multi-account configuration, OS keyring, explicit env fallback, hidden/stdin login entry, login-before-save, status/logout/list/use.
- M2: strict TLS, LOGIN, capability checks before and after authentication, command watchdog plus socket deadlines, folder list, EXAMINE, UID search/fetch, BODY.PEEK, stable IDs and UIDVALIDITY rejection. The in-memory TLS integration test proves PEEK does not mark a message read; the dialect simulator proves an untagged `* BAD Command!` cannot hang login.
- M3: go-message primary parser, enmime fallback, Chinese charset wiring, sanitized HTML, text fallback, attachment metadata, a 64 MiB message boundary, and a 40-case synthetic MIME/charset corpus.
- M4: folder/envelope/message/attachment commands, UID pagination, exact post-filtering for `--since`, server/client-window search paths, batch message reads and one-based attachment indices.
- M5: doctor checks configuration, credential source, strict TLS/login, pre/post-login capabilities, custom-server warning and Windows UTF-8 guidance. Full QQ-specific error-string mapping remains dependent on M8 evidence.
- M6: `.eml` export, safe paths, SHA-256, account HMAC key in keyring, single-manifest merge, idempotent resume, offline verification and tamper tests; manifest/plan schemas are embedded.
- M7: `agent-info`, `schema`, valid distributable `skills/qqmailctl/SKILL.md`, bilingual top-level guidance and docs disclaimer.
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

## v0.2 implementation progress

- SQLite index foundation: `modernc.org/sqlite` v1.57.0, numbered `PRAGMA user_version` migration, WAL, 5-second busy timeout, OS-level single-writer file lock, external-content FTS5, per-folder UID watermarks, UIDVALIDITY reset semantics, and audit storage.
- `sync`: all selectable folders are read with EXAMINE/PEEK semantics; new UIDs are fetched above `last_seen_uid`, the recent 200-message window refreshes flags, and classification headers are fetched without downloading bodies. `--cache-previews` and `--cache-bodies` are explicit opt-ins; both fields remain SQL NULL by default.
- `search <query> --local`: FTS5 local search with an application-generated character-bigram shadow column for Chinese queries.
- Contract schemas and command-level schema tests are present for `sync` and `search`.

## Externally blocked / deliberately not performed

- S4, the S5 write phase, the second S7 web-option snapshot, S8, S9 and S11 remain gated/manual. The read-only observations above do not replace dedicated-account write/rate-limit evidence.
- macOS Keychain and a headless Linux Secret Service failure path still need platform CI/real-host confirmation.
- Remote GitHub repository creation, first commit, push, tag, npm/PyPI/crates reservation and v0.1.0 publication require the owner's accounts/authorization and were not attempted.
- The sampled metadata-only QQ UID FETCH response was well formed; the historical malformed full-FETCH variant remains a pending fixture until an exact redacted shape is captured. The implemented timeout guard prevents indefinite blocking.
