import Link from "next/link";
import { notFound } from "next/navigation";

import { listAllOrgs } from "../../../lib/api";
import { getToken, requireSession } from "../../../lib/session";
import { NewOrgForm } from "./NewOrgForm";

// AdminOrganizationsPage is the platform-admin console for onboarding customers:
// it lists every org (not just the admin's own memberships) with SSO status, lets
// a platform admin create a customer org, and links through to per-org SSO linking
// and admin handoff. Platform admins only — separate from a customer's own
// /orgs/[slug]/settings.
export default async function AdminOrganizationsPage() {
  const user = await requireSession();
  if (!user.isAdmin) notFound();

  const token = await getToken();
  if (!token) notFound();

  const res = await listAllOrgs(token);
  const orgs = res.ok ? res.data.organizations : [];

  return (
    <>
      <div className="crumbs">
        <span>Admin</span> <span>/</span> <span>Organizations</span>
      </div>
      <div className="top">
        <h1>Organizations</h1>
      </div>
      <p className="subtle">
        Onboard customers and link their SSO. Platform admins only — this is
        separate from each org&apos;s own settings.
      </p>

      {!res.ok && <div className="banner">{res.message}</div>}

      <section className="settings-section settings-card">
        <div
          className="settings-head"
          style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16 }}
        >
          <h2 className="settings-title">All organizations</h2>
          <NewOrgForm />
        </div>
        {orgs.length === 0 ? (
          <div className="empty">No organizations yet.</div>
        ) : (
          <table className="policy-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Slug</th>
                <th>SSO</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {orgs.map((o) => (
                <tr key={o.slug}>
                  <td>{o.name}</td>
                  <td className="mono">{o.slug}</td>
                  <td>
                    {o.ssoConfigured ? (
                      <span className="badge success">
                        Linked{o.emailDomain ? ` · ${o.emailDomain}` : ""}
                      </span>
                    ) : (
                      <span className="badge">Not linked</span>
                    )}
                  </td>
                  <td className="policy-row-actions">
                    <Link className="btn ghost small" href={`/admin/organizations/${o.slug}`}>
                      Manage
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}
