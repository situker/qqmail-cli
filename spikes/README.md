# Read-only QQ Mail spikes

The release-blocking S1, S2, S3, S7, and S4 probes require the dedicated test account defined in `TECHNICAL_PLAN.md`. Run them only with `QQMAILCTL_E2E=1`, `QQMAILCTL_TEST_EMAIL`, and `QQMAILCTL_AUTH_CODE` set in the current process. Stop S4 at the first sign of throttling; do not retry authentication failures.

Record only redacted capability sets, folder names approved for publication, search behavior, and categorized error strings under `docs/compat/qq-YYYYMMDD.md`. Never record credentials or real message content.

