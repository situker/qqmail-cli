# qqmailctl implementation status

Updated: 2026-09-01

This repository implements the v0.1 read-only product surface from `TECHNICAL_PLAN.md`. It has not been tagged or published.

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

## Externally blocked / deliberately not performed

- The complete M8 release-blocking S1/S2/S3/S7/S4 probe suite still requires a dedicated QQ Mail test account and controlled runs. The owner-operated live smoke above confirms the core read-only path but did not capture the capability/search/error/rate-limit evidence required to close those spikes.
- macOS Keychain and a headless Linux Secret Service failure path still need platform CI/real-host confirmation.
- Remote GitHub repository creation, first commit, push, tag, npm/PyPI/crates reservation and v0.1.0 publication require the owner's accounts/authorization and were not attempted.
- The QQ malformed UID FETCH variant remains a pending dialect fixture until an exact redacted response shape is captured by S1; the implemented timeout guard prevents indefinite blocking in the meantime.
