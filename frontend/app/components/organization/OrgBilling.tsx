import type { BillingView } from "../../lib/api";

import { UpgradeButton } from "./UpgradeButton";

function formatBytes(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(2)} GiB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MiB`;
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KiB`;
  return `${bytes} B`;
}

function UsageRow({
  label,
  used,
  limit,
  format,
}: {
  label: string;
  used: number;
  limit?: number;
  format: (n: number) => string;
}) {
  const pct = limit && limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : null;
  return (
    <tr>
      <td>{label}</td>
      <td className="mono">{format(used)}</td>
      <td className="mono subtle">{limit != null ? format(limit) : "unlimited"}</td>
      <td>
        {pct != null && (
          <span className={`badge ${pct >= 100 ? "red" : pct >= 80 ? "amber" : "green"}`}>
            {pct}%
          </span>
        )}
      </td>
    </tr>
  );
}

// Team is priced per seat (SUITE.md: 5 EUR/seat).
const TEAM_SEAT_EUR = 5;

function daysLeft(iso?: string): number | null {
  if (!iso) return null;
  const ms = new Date(iso).getTime() - Date.now();
  return Math.max(0, Math.ceil(ms / 86_400_000));
}

// OrgBilling is the billing surface for an organization: the plan, metered
// usage against the plan's limits, and the Team subscription call to action.
// Organizations are not free: a new org runs a 14-day Team trial, and a trial
// that ended (or a canceled subscription) locks the org until it subscribes.
export function OrgBilling({ org, billing }: { org: string; billing: BillingView }) {
  const { usage, limits } = billing;
  const seats = Math.max(1, billing.seats ?? 1);
  const left = daysLeft(billing.trialEndsAt);
  const canSubscribe = ["free", "trial", "lapsed"].includes(billing.plan);
  return (
    <div>
      {billing.locked ? (
        <div className="banner danger">
          {billing.plan === "lapsed"
            ? "This organization's Team subscription has ended."
            : "This organization's free trial has ended."}{" "}
          Repositories stay readable, but creating repositories, running pipelines and inviting
          members is paused until you subscribe.
        </div>
      ) : billing.plan === "trial" && left != null ? (
        <div className="banner">
          Team trial: {left} {left === 1 ? "day" : "days"} left. Subscribe before it ends to keep
          building without interruption.
        </div>
      ) : null}
      <p>
        Plan: <span className="badge accent">{billing.plan}</span>
        {billing.plan === "team" && (
          <span className="subtle"> · {seats} {seats === 1 ? "seat" : "seats"}</span>
        )}
      </p>
      <table className="policy-table">
        <thead>
          <tr>
            <th>Resource</th>
            <th>Used</th>
            <th>Limit</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <UsageRow
            label="Repositories"
            used={usage.repos}
            limit={limits?.repos}
            format={(n) => String(n)}
          />
          <UsageRow
            label="Git storage"
            used={usage.storageBytes}
            limit={limits?.storageBytes}
            format={formatBytes}
          />
          <UsageRow
            label="Pipeline minutes (this month)"
            used={Math.round(usage.pipelineMinutes * 10) / 10}
            limit={limits?.pipelineMinutes}
            format={(n) => `${Math.round(n * 10) / 10} min`}
          />
        </tbody>
      </table>
      {canSubscribe && (
        <>
          <p className="subtle">
            Team: €{TEAM_SEAT_EUR} per seat per month · {seats} {seats === 1 ? "member" : "members"} today
            (€{TEAM_SEAT_EUR * seats}/month).
          </p>
          <UpgradeButton
            endpoint={`/orgs/${encodeURIComponent(org)}/billing/checkout`}
            label={billing.plan === "lapsed" ? "Resubscribe to Team" : "Subscribe to Team"}
          />
        </>
      )}

      <p className="subtle">
        Usage is metered from the Quill database (repos, pipeline run durations)
        and Forgejo repository sizes (git storage).
        {billing.plan === "free"
          ? " The free plan is a hard cap — creating repositories and triggering pipelines stops when a limit is reached."
          : canSubscribe
            ? ""
            : " This plan has no hard caps; usage is invoiced monthly."}
      </p>
    </div>
  );
}
