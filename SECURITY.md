# Security Policy

We take the security of **SV-Memory** seriously. Since this tool actively handles developer workspace context, memory logs, and structural code graphs, safeguarding sensitive data is our top priority.

---

## 1. Supported Versions

We actively support and patch security issues for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| 0.7.x   | :white_check_mark: |
| < 0.7.0 | :x:                |

> Policy: **latest minor only.** Security fixes land on the current minor release
> (e.g. 0.7.x). Older minors are expected to upgrade.

---

## 2. Reporting a Vulnerability

**Please do not open a public issue on GitHub for security vulnerabilities.**

If you discover a security vulnerability (such as a flaw in the secret sanitization/redaction engine or potential path traversal in code graph queries), please report it to us privately:

1.  **Draft a Security Advisory:** If the repository is hosted on GitHub, go to the **Security** tab of the repository and select **Advisories -> New draft advisory**. This allows us to discuss and patch the issue in private.
2.  **Email:** Alternatively, you can contact the SVTech team or the repository maintainers directly via email at `security@svtech.software`.

We will acknowledge your report within **48 hours** and provide a detailed timeline for a patch.

---

## 3. Important Notice on Secret Redaction

SV-Memory includes an active sanitization engine (located in [security.go](internal/security/security.go)) that uses regex patterns to redact sensitive credentials (such as API keys, JWTs, private keys, database connection strings) to `[REDACTED_SECRET]` before saving.

*   While this engine is designed to capture standard credentials, **it is not a replacement for good security practices**.
*   Avoid saving raw credentials in progress journals, error logs, or source code files whenever possible.
