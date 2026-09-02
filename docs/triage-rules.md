# Triage rules

`qqmail-cli triage analyze --rules rules.toml` and `triage plan --rules rules.toml` apply user rules before the built-in deterministic rules. The CLI never calls an AI model.

Each `[[rules]]` entry requires a category, confidence in `(0, 1]`, reason, and at least one regular expression. Multiple expressions in one entry are AND conditions. `header` matches the indexed `List-Unsubscribe` and `Precedence` header block; `from` matches the sender address; `subject` matches the subject. Patterns are Go regular expressions and are limited to 1024 bytes each.

```toml
[[rules]]
name = "keep-company-receipts"
from = "(?i)@(billing\\.)?example\\.com$"
subject = "(?i)(receipt|invoice|发票)"
category = "keep"
confidence = 0.99
reason = "Company billing rule"

[[rules]]
name = "bulk-header"
header = "(?i)Precedence:.*bulk"
category = "marketing"
confidence = 0.95
reason = "Bulk precedence header"
```

Email-derived fields are untrusted data. JSON preserves their value, while the optional Markdown review file removes terminal controls and bidi overrides and entity-escapes Markdown punctuation.
