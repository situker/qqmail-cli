# QQ Mail read-only spike observations — 2026-09-01

Status: owner-operated, redacted Windows observation using the configured OS-keyring account. These are `实测兼容（2026-09-01）` observations for one account and date, not Tencent guarantees. The ignored local result retains mailbox counts and a UID-set hash; this committed record deliberately omits them.

## S1 — IMAP capabilities and dialect

- The greeting advertised IMAP4/IMAP4rev1, NAMESPACE, ID, and AUTH mechanisms including LOGIN and PLAIN.
- Post-login CAPABILITY advertised MOVE, UIDPLUS, IDLE, ID, NAMESPACE, COMPRESS=DEFLATE, and no LITERAL+.
- An intentionally unsupported command still produced the illegal untagged `* BAD Command!` without a tagged completion. The product watchdog remains required.
- A sampled metadata-only `UID FETCH (UID FLAGS RFC822.SIZE)` response had normal parenthesis/spacing shape. No malformed FETCH fixture was captured in this sample.

## S2 — authentication and ID

- LOGIN succeeded.
- AUTHENTICATE PLAIN also succeeded; the older community claim that AUTHENTICATE is universally rejected is not true for this observation.
- RFC 2971 ID was accepted after login.
- Product policy remains LOGIN as the single operational path to minimize behavior variance; ID may be sent only behind runtime capability detection.

## S3 — SEARCH matrix

- ASCII SUBJECT, FROM, SINCE, OR, and HEADER commands returned tagged OK.
- A UTF-8 Chinese query sent directly as a quoted string returned tagged OK.
- A synchronizing literal query did not receive the required continuation; the server immediately returned SEARCH data and a tagged OK. That result is not trustworthy as a match for the intended literal payload because the payload was never sent.
- qqmail-cli therefore uses `client_window` filtering for non-ASCII `--subject`/`--from` filters. ASCII filters may continue to use server search with the existing error fallback.

## S5 — UID state, read-only phase

- INBOX UIDVALIDITY was non-zero and stable across two fresh sessions.
- UIDPLUS was advertised.
- COPYUID and UID EXPUNGE behavior remain `待人工触发`; the gated synthetic write phase was not run. Fallback code must not use bare EXPUNGE.

## S6 — IDLE

- IDLE was accepted, remained open for a bounded 10-second observation, and completed with tagged OK after DONE.
- This short observation is insufficient to claim long-term reliability. Product `watch` remains polling-only.

## S7 — web collection options

- A privacy-preserving current-setting baseline was captured locally without storing folder names or message IDs in the repository.
- A second snapshot after changing exactly one QQ web collection option remains `待人工触发`; no visibility conclusion is claimed yet.

## S10 — SMTP STARTTLS transport

- Port 587 advertised STARTTLS before authentication.
- Strict certificate verification succeeded and negotiated TLS 1.3 in this observation.
- No SMTP authentication or message send was performed.

## Still pending

- S4 login-frequency ladder, S5 write phase, S7 option comparison, S8 new-IP host, S9 quota send, and S11 Chinese-folder writes.
- SMTP 465 authenticated sending and all real write tests remain gated.

No account address, authorization code, message content, subject, folder name, message identifier, or raw protocol log is stored in this record.
