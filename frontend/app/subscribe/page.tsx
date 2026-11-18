import Link from "next/link";
import { redirect } from "next/navigation";

import { UpgradeButton } from "../components/organization/UpgradeButton";
import { AppTile } from "../components/icons/AppMarks";
import { listOrgs } from "../lib/api/organizations";
import { needsPlan } from "../lib/paywall";
import { getToken, requireSession } from "../lib/session";

// The paywall: Quill has no free plan, so a new account picks Starter, Pro or
// Team before anything else. Outside the (app) shell so its "no plan -> here"
// redirect cannot loop. After paying, Creem returns to ?upgraded=1 while the
// webhook may still be in flight, so that state refreshes until the plan lands.
export default async function SubscribePage({ searchParams }: { searchParams: { upgraded?: string } }) {
  await requireSession();
  const token = await getToken();
  if (!token) redirect("/sign-in");
  const orgs = await listOrgs(token);
  const gated = await needsPlan(token, orgs);
  if (!gated) redirect("/");
  const confirming = Boolean(searchParams.upgraded);

  return (
    <div className="auth-page">
      {confirming && <meta httpEquiv="refresh" content="4" />}
      <div className="signin-card paywall-card">
        <div className="auth-brand">
          <AppTile app="quill" size={28} /> Quill
        </div>
        <h1 className="signin-title">Choose your plan</h1>
        <p className="signin-sub">
          Quill has no free plan. Pick one to get started — per month, VAT included, cancel any time.
        </p>
        {confirming && (
          <div className="banner">Payment received? Confirming it with Creem — this page refreshes by itself.</div>
        )}
        <div className="plan-actions">
          <div>
            <b>Starter · €4</b>
            <p className="subtle">5 GB git storage, 100 CI minutes, private repositories.</p>
            <UpgradeButton endpoint="/me/billing/checkout" plan="starter" label="Get Starter" />
          </div>
          <div>
            <b>Pro · €9</b>
            <p className="subtle">10 GB + 1 GB LFS, 500 CI minutes, self-hosted runners, priority support.</p>
            <UpgradeButton endpoint="/me/billing/checkout" plan="pro" label="Get Pro" />
          </div>
          <div>
            <b>Team · €11 per user</b>
            <p className="subtle">For organizations: SSO, member management, org-wide policies.</p>
            <Link className="btn gold" href="/onboarding">
              Set up a team
            </Link>
          </div>
        </div>
        <p className="signin-foot">
          Have a beta invite? Open your invite link again. <Link href="/api/auth/signout">Sign out</Link>
        </p>
      </div>
    </div>
  );
}
