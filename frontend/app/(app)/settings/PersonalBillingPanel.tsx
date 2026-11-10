import { authGet } from "../../lib/api/client";
import { getToken } from "../../lib/session";
import { UpgradeButton } from "../../components/organization/UpgradeButton";

function formatBytes(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(2)} GiB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MiB`;
  if (bytes >= 1 << 10) return `${(bytes / (1 << 10)).toFixed(1)} KiB`;
  return `${bytes} B`;
}

// PersonalBillingPanel is the account settings' billing surface: the personal
// plan, metered usage against it, and the Pro upgrade checkout. Accounts on the
// legacy shared tenant see the unlimited internal plan — no caps, no upsell.
export async function PersonalBillingPanel() {
  const token = await getToken();
  if (!token) return null;
  const res = await authGet<{
    plan: string;
    usage: { repos: number; storageBytes: number; pipelineMinutes: number };
    limits?: {
      repos?: number;
      storageBytes?: number;
      pipelineMinutes?: number;
    };
  }>(token, "/api/v1/me/billing");
  if (!res.ok) return null;
  const { plan, usage, limits } = res.data;

  const rows: { label: string; used: number; limit?: number; format: (n: number) => string }[] = [
    { label: "Repositories", used: usage.repos, limit: limits?.repos, format: (n) => String(n) },
    {
      label: "Git storage",
      used: usage.storageBytes,
      limit: limits?.storageBytes,
      format: formatBytes,
    },
    {
      label: "Pipeline minutes (this month)",
      used: Math.round(usage.pipelineMinutes * 10) / 10,
      limit: limits?.pipelineMinutes,
      format: (n) => `${Math.round(n * 10) / 10} min`,
    },
  ];

  return (
    <div className="panel form-narrow">
      <h2>Billing</h2>
      <div className="readme-body">
        <p>
          Plan: <span className={`badge ${plan === "free" ? "" : "gold"}`}>{plan}</span>
        </p>
        <table className="policy-table">
          <thead>
            <tr>
              <th>Resource</th>
              <th>Used</th>
              <th>Limit</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.label}>
                <td>{r.label}</td>
                <td className="mono">{r.format(r.used)}</td>
                <td className="mono subtle">{r.limit != null ? r.format(r.limit) : "unlimited"}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {plan === "free" && (
          <p className="subtle">
            The free plan is a hard cap — creating repositories and triggering
            pipelines stops when a limit is reached.
          </p>
        )}
        {plan === "free" && (
          <UpgradeButton endpoint="/me/billing/checkout" label="Upgrade to Pro" />
        )}
      </div>
    </div>
  );
}
