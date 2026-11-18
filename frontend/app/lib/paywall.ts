import { authGet } from "./api/client";
import type { Organization } from "./api/organizations";

// needsPlan reports whether a signed-in account must pick a plan before using
// Quill: its personal plan is "inactive" (never subscribed) and it belongs to no
// organization (members of a team keep working under the team's plan; the org's
// own billing page handles an unpaid team). Lapsed accounts are not gated: their
// repositories stay readable and the billing banner explains the lock. A failed
// lookup never gates, so an API hiccup can't lock people out.
export async function needsPlan(token: string, orgs: Organization[]): Promise<boolean> {
  if (orgs.length > 0) return false;
  const res = await authGet<{ plan: string }>(token, "/api/v1/me/billing");
  return res.ok && res.data.plan === "inactive";
}
