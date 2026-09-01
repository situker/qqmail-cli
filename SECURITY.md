# Security Policy

Please report security issues privately to the repository owner. Do not include authorization codes, complete email bodies, real attachments, or unredacted logs in an issue.

Read paths open folders with IMAP `EXAMINE` and fetch bodies with `BODY.PEEK`. Server-write calls are isolated to one IMAP boundary and one policy call site, guarded by static and wire tests; no bare EXPUNGE or user-facing permanent-delete command exists.

Set `QQMAILCTL_READONLY=1` in Agent sessions. It blocks every local/server mutation and real SMTP send before credential access or network dialing. SMTP execution additionally requires a non-empty recipient allowlist and real-TTY human confirmation. Email-derived fields are untrusted and are sanitized in human-rendered confirmation surfaces.
