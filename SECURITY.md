# Security Policy

Please report security issues privately to the repository owner. Do not include authorization codes, complete email bodies, real attachments, or unredacted logs in an issue.

v0.1 is intentionally read-only: folders are opened with IMAP `EXAMINE`, message bodies are fetched with `BODY.PEEK`, and no mutation commands are exposed by the protocol interface or CLI.

