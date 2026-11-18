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

// Team Pro is priced per user: EUR 11 per user per month, VAT included.
const TEAM_SEAT_EUR = 11;

// OrgBilling is the billing surface for an organization: the plan, metered
// usage against the plan's limits, and the Team Pro subscription call to
// action. There is no free tier or trial: a new org is locked until it
// subscribes, and so is one whose subscription was canceled.
export function OrgBilling({ org, billing }: { org: string; billing: BillingView }) {
  const { usage, limits } = billing;
  const seats = Math.max(1, billing.seats ?? 1);
  const canSubscribe = ["inactive", "lapsed", "beta"].includes(billing.plan);
  return (
    <div>
      {billing.locked ? (
        <div className="banner danger">
          {billing.plan === "lapsed"
            ? "This organization's Team Pro subscription has ended."
            : "Quill has no free plan: this organization needs a Team Pro subscription."}{" "}
          Repositories stay readable, but creating repositories, running pipelines and inviting
          members is paused until you subscribe.
        </div>
      ) : null}
      <p>
        Plan: <span className="badge accent">{billing.plan}</span>
        {billing.plan === "team" && (
          <span className="subtle">
            {" "}· {seats} {seats === 1 ? "member" : "members"}
            {billing.billedSeats ? ` · ${billing.billedSeats} billed` : ""}
            {billing.billedSeats && billing.billedSeats !== seats ? " (syncing)" : ""}
          </span>
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
            Team Pro: €{TEAM_SEAT_EUR} per user per month, VAT included · {seats} {seats === 1 ? "member" : "members"} today
            (€{TEAM_SEAT_EUR * seats}/month).
          </p>
          <UpgradeButton
            endpoint={`/orgs/${encodeURIComponent(org)}/billing/checkout`}
            label={billing.plan === "lapsed" ? "Resubscribe to Team Pro" : "Subscribe to Team Pro"}
          />
        </>
      )}

      {billing.plan === "team" && (
        <p className="subtle">
          Seats follow your members: adding or removing someone updates the subscription, and the
          difference is prorated on your next invoice.
        </p>
      )}
      <p className="subtle">
        Usage is metered from the Quill database (repos, pipeline run durations)
        and Forgejo repository sizes (git storage).
        {limits ? " Plan limits are hard caps — creating repositories and triggering pipelines stops when one is reached." : ""}
      </p>
    </div>
  );
}
