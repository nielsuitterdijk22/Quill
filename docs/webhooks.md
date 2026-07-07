# Webhooks

Webhooks let your systems react to events in Quill — a PR opened, a pipeline finished, a branch merged. Quill POSTs a JSON payload to your URL whenever the event fires.

## Create a webhook

Go to **repo Settings → Webhooks → Add webhook** (for repo-scoped events) or **org Settings → Webhooks → Add webhook** (for org-wide events).

- **Payload URL** — where Quill sends the POST
- **Secret** — used to sign the request (recommended)
- **Events** — which events trigger this webhook

## Events

| Event                 | When it fires                              |
| --------------------- | ------------------------------------------ |
| `push`                | Commits pushed to any branch               |
| `pull_request`        | PR opened, closed, merged, or synchronized |
| `pull_request_review` | Review submitted                           |
| `pipeline_run`        | Pipeline run started, completed, or failed |
| `branch`              | Branch created or deleted                  |
| `member`              | Org or project membership changed          |

## Payload

Every webhook payload includes:

```json
{
  "event": "pull_request",
  "action": "opened",
  "sender": {
    "login": "jane",
    "url": "https://quill.so/jane"
  },
  "org": "acme",
  "repo": {
    "name": "payments",
    "full_name": "acme/payments",
    "url": "https://quill.so/acme/payments"
  },
  "pull_request": { ... }
}
```

The shape of the nested object depends on the event type.

## Verify the signature

Quill signs every payload with HMAC-SHA256 using your secret. Always verify it:

```python
import hmac, hashlib

def verify(payload_body: bytes, header: str, secret: str) -> bool:
    expected = "sha256=" + hmac.new(
        secret.encode(), payload_body, hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, header)

sig = request.headers["X-Quill-Signature"]
if not verify(request.body, sig, YOUR_SECRET):
    abort(401)
```

The signature header is `X-Quill-Signature`.

## Delivery and retries

Quill considers a delivery successful if your endpoint responds with any 2xx status within 10 seconds.

On failure (non-2xx, timeout, connection error), Quill retries with exponential backoff:

- 1 min, 5 min, 30 min, 2 hours, 8 hours

After 5 failed attempts, the delivery is marked failed. You can manually redeliver any payload from the webhook's **Recent deliveries** tab. 5 failed attempts, the delivery is marked failed. You can manually redeliver any payload from the webhook's **Recent deliveries** tab.

## Recent deliveries

Go to **Settings → Webhooks → your webhook → Recent deliveries** to see all recent payloads, their response status and body, and to manually redeliver any of them.

## Related

- [External checks](policies/external-checks.md)
