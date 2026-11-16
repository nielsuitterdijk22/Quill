import { redirect } from "next/navigation";

// /orgs/{org}/settings opens the first section. Old links with ?upgraded=1
// (Creem checkout return) land on billing.
export default function OrgSettingsIndex({
  params,
  searchParams,
}: {
  params: { org: string };
  searchParams: { upgraded?: string };
}) {
  const base = `/orgs/${encodeURIComponent(params.org)}/settings`;
  redirect(searchParams.upgraded ? `${base}/billing?upgraded=1` : `${base}/members`);
}
