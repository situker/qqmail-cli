# Changelog

All notable development changes are recorded here. Formal releases remain owner-controlled.

## Unreleased

### Added

- Rerunnable S1-S11 probe entry points with OS-keyring credential loading, redacted ignored results, and dual gates for rate/write observations.
- A combined read-only probe suite that minimizes QQ login churn.
- Redacted 2026-09-01 capability, authentication, search, UID, IDLE, visibility-baseline, and SMTP STARTTLS observations.
- A pure-Go SQLite metadata index with WAL, migrations, single-writer locking, UIDVALIDITY reset handling, and FTS5 Chinese bigram search.
- Incremental `sync` and `search --local` commands with embedded output schemas.

### Changed

- Non-ASCII sender/subject filtering now uses the deterministic client window because QQ's synchronizing literal SEARCH continuation was non-compliant in the live observation.
- IMAP sessions send RFC 2971 ID only when the server advertises the capability.

### Security

- Probe results are excluded from Git and omit credentials, addresses, content, subjects, folder names, and message IDs.
- S4/S5-write/S9/S11 require both an explicit write switch and dedicated-test-account confirmation.
- Message previews and bodies remain absent from the cache unless explicitly enabled; opted-in cache content is documented as unencrypted.
