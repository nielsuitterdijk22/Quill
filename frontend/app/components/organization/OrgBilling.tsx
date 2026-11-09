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

// OrgBilling is the read-only billing surface for an organization: the current
// plan (set by the platform operator) and metered usage against the plan's
// limits. Free-tier tenants see hard caps (50 repos, 1 GiB, 100 pipeline
// min/mo); other plans are usage-based with no hard cap.
export function OrgBilling({ org, billing }: { org: string; billing: BillingView }) {
  const { usage, limits } = billing;
  return (
    <div>
      <p>
        Plan: <span className="badge accent">{billing.plan}</span>
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
      {billing.plan === "free" && <UpgradeButton org={org} />}

      <p className="subtle">
        Usage is metered from the Quill database (repos, pipeline run durations)
        and Forgejo repository sizes (git storage).
        {billing.plan === "free"
          ? " The free plan is a hard cap — creating repositories and triggering pipelines stops when a limit is reached."
          : " This plan has no hard caps; usage is invoiced monthly."}
      </p>
    </div>
  );
}
