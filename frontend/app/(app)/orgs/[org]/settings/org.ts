import { notFound } from "next/navigation";

import { listOrgs, type Organization } from "../../../../lib/api";
import { getToken } from "../../../../lib/session";

// loadOrg resolves the signed-in caller's membership of an organization (the
// access check for every settings page: non-members get notFound).
export async function loadOrg(slug: string): Promise<{ token: string; org: Organization; canEdit: boolean }> {
  const token = await getToken();
  if (!token) notFound();
  const orgs = await listOrgs(token);
  const org = orgs.find((o) => o.slug === slug);
  if (!org) notFound();
  return { token, org, canEdit: org.role === "admin" };
}
