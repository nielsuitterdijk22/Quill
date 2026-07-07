# Time windows

A time window gate restricts merges to specific times. Outside the window, the merge button is disabled and the PR shows which window is blocking.

## When to use this

- Prevent deployments late on Friday afternoons
- Restrict production changes to business hours
- Enforce a "merge only during the daily sync" rule for coordinated releases

## Configure a time window

In your branch policy, add a `time_window` gate:

```text
Gate: time_window
Days: Monday, Tuesday, Wednesday, Thursday, Friday
Start: 09:00
End: 17:00
Timezone: Europe/Amsterdam
```

All times are interpreted in the configured timezone. Quill converts to UTC internally.

## Multiple windows

You can define multiple windows if your allowed merge times aren't contiguous:

```text
Gate: time_window
Windows:
  - days: [Mon, Tue, Wed, Thu, Fri]
    start: 09:00
    end: 12:00
  - days: [Mon, Tue, Wed, Thu, Fri]
    start: 14:00
    end: 17:00
```

This allows merges in the morning and afternoon but not during the midday block.

## Time window vs. change freeze

| | Time window | Change freeze |
|---|---|---|
| **Type** | Recurring schedule | One-off period |
| **Set by** | Policy configuration | On-demand (owners) |
| **Lifted by** | Waiting until the window opens | Owner action |
| **Use case** | Always restrict to business hours | Block merges during a deployment, incident, or release |

Use time windows for permanent recurring restrictions. Use [change freeze](change-freeze.md) for exceptional one-off blocks.

## Override

Time windows can be overridden by org or project owners on a per-PR basis. Click **Override time window** on the PR — you'll be asked for a reason, which is recorded in the audit log.

## Supported timezones

Quill accepts any IANA timezone name (e.g. `America/New_York`, `Europe/London`, `Asia/Tokyo`). The UI shows a searchable dropdown when you're setting up the gate.
