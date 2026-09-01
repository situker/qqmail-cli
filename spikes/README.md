# QQ Mail spikes

S1 through S11 have rerunnable PowerShell entry points in this directory and a cross-platform Go core at `spikes/qqprobe`. The Go core reads the selected account from the normal qqmailctl configuration and gets its authorization code from the OS keyring. No credential flag or result field exists.

Results default to `spikes/results/`, which Git ignores. Review them locally, then write only redacted conclusions under `docs/compat/qq-YYYYMMDD*.md`. Never commit account addresses, authorization codes, message bodies, subjects, folder names, message IDs, or unredacted protocol logs. Label observations `实测兼容（YYYY-MM-DD）`, never as provider guarantees.

| Spike | Entry point | Default behavior |
|---|---|---|
| S1 | `s01-capability.ps1` | Read-only CAPABILITY/BAD/FETCH-shape observation |
| S2 | `s02-auth-id.ps1` | Read-only LOGIN/AUTHENTICATE/ID observation, no retries |
| S3 | `s03-search-matrix.ps1` | Read-only SEARCH matrix |
| S4 | `s04-login-frequency.ps1` | Pending unless both safety gates and `-ExecuteProbe` are present |
| S5 | `s05-uidvalidity-uidplus.ps1` | UIDVALIDITY read-only phase; isolated synthetic write phase is gated |
| S6 | `s06-idle-observe.ps1` | Bounded read-only IDLE observation |
| S7 | `s07-collection-options.ps1` | Privacy-preserving visibility snapshot; web option changes are manual |
| S8 | `s08-new-ip-login.ps1` | Must run on the target remote host/IP |
| S9 | `s09-smtp-quota.ps1` | Gated, interactive, self-addressed low-rate sends only |
| S10 | `s10-smtp-starttls.ps1` | Unauthenticated STARTTLS transport observation |
| S11 | `s11-chinese-folder.ps1` | Gated create/rename/delete of one probe-created folder |

Write/rate probes require both `QQMAILCTL_E2E_WRITE=1` and `QQMAILCTL_DEDICATED_TEST_ACCOUNT=1`. S9 additionally requires `QQMAILCTL_TEST_RECIPIENT` to exactly equal the configured default account. These gates do not authorize touching pre-existing mail or folders.
