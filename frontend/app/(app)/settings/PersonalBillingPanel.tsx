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
// plan, metered usage against it, and the Starter / Pro checkout. There is no
// free tier: a new account is "inactive" (locked) until it subscribes or a
// hand-issued beta link moves it to the free "beta" plan. Accounts on the
// legacy shared tenant see the unlimited internal plan — no caps, no upsell.
export async function PersonalBillingPanel() {
  const token = await getToken();
  if (!token) return null;
  const res = await authGet<{
    plan: string;
    locked?: boolean;
    usage: { repos: number; storageBytes: number; pipelineMinutes: number };
    limits?: {
      repos?: number;
      storageBytes?: number;
      pipelineMinutes?: number;
    };
  }>(token, "/api/v1/me/billing");
  if (!res.ok) return null;
  const { plan, usage, limits, locked } = res.data;
  const canSubscribe = ["inactive", "lapsed", "beta"].includes(plan);

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
        {locked && (
          <div className="banner danger">
            {plan === "lapsed" ? "Your subscription has ended." : "Quill has no free plan."} Your repositories stay
            readable, but creating repositories and running pipelines is paused until you choose a plan.
          </div>
        )}
        <p>
          Plan: <span className={`badge ${locked ? "red" : "gold"}`}>{plan}</span>
          {plan === "beta" && <span className="subtle"> · free during the beta, with Pro limits</span>}
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
        {canSubscribe && (
          <>
            <p className="subtle">
              Per month, VAT included. Cancel any time.
            </p>
            <div className="plan-actions">
              <div>
                <b>Starter · €4</b>
                <p className="subtle">5 GB of git storage, 100 CI minutes, private repositories, SBOM export.</p>
                <UpgradeButton endpoint="/me/billing/checkout" plan="starter" label="Get Starter" />
              </div>
              <div>
                <b>Pro · €9</b>
                <p className="subtle">10 GB + 1 GB LFS, 500 CI minutes, self-hosted runners, priority support.</p>
                <UpgradeButton endpoint="/me/billing/checkout" plan="pro" label="Get Pro" />
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
