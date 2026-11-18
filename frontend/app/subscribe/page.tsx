import Link from "next/link";
import { redirect } from "next/navigation";

import { AppTile } from "../components/icons/AppMarks";
import { listOrgs } from "../lib/api/organizations";
import { PlanPicker } from "./PlanPicker";
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
          Quill has no free plan. Pick one to get started.
        </p>
        {confirming && (
          <div className="banner">Payment received? Confirming it with Creem — this page refreshes by itself.</div>
        )}
        <PlanPicker />
        <p className="signin-foot">
          Have a beta invite? Open your invite link again. <Link href="/api/auth/signout">Sign out</Link>
        </p>
      </div>
    </div>
  );
}
