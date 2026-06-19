# Privacy Policy — Quill instance operated by [Operator Name]

> **How to use this template:** Replace every `[placeholder]` with your
> own details. Delete or adjust sections that do not apply to your
> deployment. This template is provided by the Quill project as a
> starting point; you are responsible for ensuring the final policy
> complies with the laws of your jurisdiction and the actual behaviour
> of your instance. Seek legal advice if you are unsure.

---

**Last updated:** [DATE]

This privacy policy describes how **[Operator Name]** ("we", "us", "our"),
operator of the Quill instance at **[INSTANCE URL]**, collects, uses, and
stores personal data when you use this service.

---

## 1. Who we are (controller)

| Field               | Details                     |
| ------------------- | --------------------------- |
| Operator / controller | [Your name or organisation] |
| Address             | [Your postal address]       |
| Email               | [Your contact email]        |
| Data Protection Officer (if applicable) | [DPO name and contact, or "Not applicable"] |

---

## 2. What data we collect and why

### 2.1 Account data

When you register, we store:

- **Username** — to identify you within the platform.
- **Email address** — for account management (password reset if SMTP is
  configured, operator contact).
- **Display name** — shown on pull requests and comments.
- **Hashed password** — stored using bcrypt; we never store your plaintext
  password.
- **Account creation timestamp.**

Legal basis (GDPR Article 6): **contract performance** — this data is
necessary to provide the service you signed up for.

### 2.2 Repository and code data

Code, commits, pull requests, comments, and pipeline configuration you push
or create are stored on this server. You control what you upload.

Legal basis: **contract performance**.

### 2.3 Git access tokens

If you create personal git tokens (for HTTPS clone/push), we store the token
name and creation timestamp. The token secret is stored in Forgejo and is
not visible after creation.

Legal basis: **contract performance**.

### 2.4 Pipeline run logs

When a CI pipeline runs, execution logs (stdout/stderr of each step) are
stored. These may contain information from your repository or environment
variables you configure.

Legal basis: **contract performance**.

### 2.5 Server logs

Our web server records standard access logs (IP address, timestamp, HTTP
method, path, status code, response size). These are used for debugging and
security monitoring.

Legal basis: **legitimate interests** (security and reliability).

Retention: server logs are retained for **[X days]** then deleted
automatically.

---

## 3. What we do NOT collect

- We do not use analytics, tracking pixels, or telemetry.
- We do not load fonts, scripts, or other resources from third-party CDNs.
  All assets are served from this server.
- We do not share your data with advertisers or data brokers.
- We do not use cookies for tracking. The only cookie set is the session
  token (`quill_token`), which is strictly necessary for you to stay logged
  in. No cookie consent banner is required under GDPR for strictly necessary
  cookies.

---

## 4. Data sharing

We do not sell or share your personal data with third parties, except:

- **Forgejo** (git backend): this instance runs a local Forgejo service that
  stores your repositories and pull requests. Forgejo is self-hosted on the
  same server and data does not leave it.
- **Legal obligations:** we may disclose data if required by law.

---

## 5. Data retention

| Data type          | Retention                              |
| ------------------ | -------------------------------------- |
| Account data       | Until you delete your account          |
| Repository data    | Until you delete the repository        |
| Pipeline logs      | [X days / until you delete the repo]   |
| Server access logs | [X days]                               |

When you delete your account (Settings → Danger zone → Delete my account),
we delete your profile, all git tokens, and your mirrored Forgejo account.
Repositories you own are [deleted / transferred to another member — choose
one and describe the procedure].

---

## 6. Your rights (GDPR Articles 15–22)

As a data subject in the European Economic Area or United Kingdom, you have
the right to:

| Right | How to exercise it |
| ----- | ------------------ |
| **Access** (Art. 15) | Download your data from Settings → Export your data, or email us. |
| **Rectification** (Art. 16) | Update your email or display name in Settings. |
| **Erasure** (Art. 17) | Delete your account from Settings → Danger zone, or email us. |
| **Portability** (Art. 20) | Download a machine-readable JSON export from Settings → Export your data. |
| **Restriction** (Art. 18) | Email us to request processing restrictions. |
| **Objection** (Art. 21) | Email us to object to processing based on legitimate interests. |

To exercise any right, contact us at **[Your contact email]**. We will
respond within **30 days**.

If you are unhappy with how we handle your data, you have the right to lodge
a complaint with your national data protection authority (e.g. the Dutch AP,
German BfDI, French CNIL, Irish DPC).

---

## 7. Security

We take reasonable technical measures to protect your data:

- Passwords are hashed with bcrypt (never stored in plaintext).
- Session cookies are `HttpOnly`, `SameSite=Lax`, and `Secure` when the
  instance is served over HTTPS.
- The API sets `Content-Security-Policy`, `X-Frame-Options`, and other
  security headers on every response.
- We recommend (and document) running this service behind a TLS-terminating
  reverse proxy.

No system is 100% secure. If you discover a security vulnerability, please
disclose it responsibly by emailing **[Your security contact email]**.

---

## 8. International transfers

All data is stored on servers located in **[Country / data centre location]**.
We do not transfer personal data to countries outside the EEA [unless you
describe an exception and the safeguards in place].

---

## 9. Children

This service is not directed at children under 16. If you believe a child
has created an account, contact us and we will delete it.

---

## 10. Changes to this policy

We will post any updates to this page with a revised "Last updated" date. For
significant changes we will notify users by [email / in-app notice].

---

## 11. Contact

Questions about this policy: **[Your contact email]**
