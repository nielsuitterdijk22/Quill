# External checks

An external check is a policy gate that reaches out to your own systems and waits for a verdict before allowing a merge. Use it to integrate change management tools, ITSM tickets, Slack approval flows, or any custom business logic into your merge process.

## How it works

1. A PR is opened (or a merge is attempted, depending on your config).
2. Quill POSTs a payload to your configured endpoint with the PR details and a unique callback token.
3. Your system does whatever it needs — looks up a JIRA ticket, pings Slack, checks a change calendar.
4. Your system POSTs back to `https://quill.so/api/gates/callback` with the token and a verdict (`pass` or `fail`).
5. The gate updates on the PR. If all other gates also pass, the merge button unlocks.

## Configure an external check

In your branch policy, add an `external_check` gate:

```text
Gate: external_check
URL: https://itsm.acme.com/quill-gate
Secret: your-webhook-secret
Trigger: on_pr_open
Timeout: 24h
```

**URL** — Quill POSTs to this endpoint when the gate triggers.

**Secret** — Quill signs the request with HMAC-SHA256 using this secret. Verify it on your end to ensure the request is genuine.

**Trigger** — when the gate fires:

- `on_pr_open` — fires when the PR is first opened
- `on_merge_attempt` — fires when someone clicks Merge
- `always` — fires on both

**Timeout** — how long Quill waits for a callback. After this, the gate fails automatically. Minimum 1 minute, maximum 7 days.

## Request payload

Quill sends a POST with `Content-Type: application/json`:

```json
{
  "token": "gk_abc123...",
  "event": "pr_open",
  "pr": {
    "number": 42,
    "title": "Add payment retry logic",
    "url": "https://quill.so/acme/payments/pulls/42",
    "author": "jane",
    "source_branch": "feature/retry-logic",
    "target_branch": "main",
    "created_at": "2026-06-25T14:30:00Z"
  },
  "repo": {
    "owner": "acme",
    "name": "payments",
    "url": "https://quill.so/acme/payments"
  },
  "policy": {
    "gate_id": "itsm-approval",
    "timeout_at": "2026-06-26T14:30:00Z"
  }
}
```

## Callback

Your system calls back to Quill when it has a verdict:

```text
POST https://quill.so/api/gates/callback
Content-Type: application/json

{
  "token": "gk_abc123...",
  "verdict": "pass",
  "message": "Change request CR-1234 approved by Jane Doe"
}
```

| Field | Values |
|-------|--------|
| `token` | The token from the original request — must match exactly |
| `verdict` | `pass` or `fail` |
| `message` | Optional. Shown on the PR to explain the verdict. |

The callback endpoint is public and requires no authentication — the token is the authentication.

## Verify the webhook signature

On your server, verify that requests actually came from Quill before processing them:

```python
import hmac, hashlib

def verify_quill_signature(payload_body: bytes, signature_header: str, secret: str) -> bool:
    expected = "sha256=" + hmac.new(
        secret.encode(), payload_body, hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, signature_header)

# In your handler:
sig = request.headers.get("X-Quill-Signature")
if not verify_quill_signature(request.body, sig, YOUR_SECRET):
    return 401
```

The signature is in the `X-Quill-Signature` request header.

## Multiple external checks

A single branch policy can have multiple external checks. All must pass. Each has its own token, so callbacks are routed to the correct gate.

## Override

An org owner can override a pending or failed external check on a specific PR by clicking **Override check** on the PR page. They must provide a reason. The override and reason are recorded in the audit log.

## Example: JIRA change ticket

```python
@app.post("/quill-gate")
def quill_gate(request):
    verify_signature(request)
    data = request.json()

    # Look up the JIRA ticket number from the PR title/body
    ticket = extract_jira_ticket(data["pr"]["title"])
    if not ticket:
        callback(data["token"], "fail", "No JIRA ticket found in PR title")
        return

    # Check if the ticket is approved
    status = jira.get_issue_status(ticket)
    if status == "Approved":
        callback(data["token"], "pass", f"{ticket} approved")
    else:
        callback(data["token"], "fail", f"{ticket} is {status}, not yet approved")
```

## Related

- [Policies overview](overview.md)
- [Change freeze](change-freeze.md)
- [Webhooks](../webhooks.md)
