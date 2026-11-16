import { notFound } from "next/navigation";

import { getOrgBilling } from "../../../../../lib/api";
import { OrgBilling } from "../../../../../components/organization/OrgBilling";
import { loadOrg } from "../org";

export default async function OrgBillingSettings({
  params,
  searchParams,
}: {
  params: { org: string };
  searchParams: { upgraded?: string };
}) {
  const { token, org, canEdit } = await loadOrg(params.org);
  if (!canEdit) notFound();
  const res = await getOrgBilling(token, params.org);
  return (
    <section className="settings-section settings-card">
      <div className="settings-head">
        <h2 className="settings-title">Billing &amp; usage</h2>
        <p className="subtle">Your plan and metered usage.</p>
      </div>
      {searchParams.upgraded && (
        <div className="banner">Thanks! Your payment is being confirmed — the plan updates within a minute.</div>
      )}
      {res.ok ? <OrgBilling org={org.slug} billing={res.data} /> : <p className="subtle">Billing is unavailable right now.</p>}
    </section>
  );
}
