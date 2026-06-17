import { notFound } from "next/navigation";

import { getTenantPolicies } from "../../../lib/api";
import { getToken, requireSession } from "../../../lib/session";
import { PolicyManager } from "../../../components/policy/PolicyManager";

// The MVP runs a single tenant; tenant-wide governance is managed under its slug.
const DEFAULT_TENANT = "default";

// AdminPoliciesPage manages tenant-scoped governance — the broadest scope. Rules
// here apply to every project and repository in the tenant. Platform admins only;
// non-admins get a 404 so the page's existence isn't leaked.
export default async function AdminPoliciesPage() {
  const user = await requireSession();
  if (!user.isAdmin) notFound();

  const token = getToken();
  if (!token) notFound();

  const res = await getTenantPolicies(token, DEFAULT_TENANT);
  if (!res.ok) {
    if (res.status === 404) notFound();
    return (
      <>
        <div className="crumbs">
          <span>Admin</span> <span>/</span> <span>Policies</span>
        </div>
        <h1>Tenant policies</h1>
        <div className="banner">{res.message}</div>
      </>
    );
  }

  const { tenant, policies } = res.data;

  return (
    <>
      <div className="crumbs">
        <span>Admin</span> <span>/</span> <span>Policies</span>
      </div>

      <div className="top">
        <h1>Tenant policies</h1>
      </div>
      <p className="subtle">
        Governance for the <b>{tenant.name}</b> tenant. These branch policies
        apply to every project and repository. Projects and repositories may add
        stricter rules but never weaken these — lock a rule to forbid loosening
        it entirely.
      </p>

      <section className="settings-section">
        <div className="settings-head">
          <h2 className="settings-title">Branch policies</h2>
          <p className="subtle">
            Set tenant-wide branch protection, e.g. require a pull request and an
            approver on <code>main</code> across the whole tenant.
          </p>
        </div>
        <PolicyManager
          target={{ scope: "tenant", tenant: tenant.slug }}
          policies={policies}
          canLock
        />
      </section>
    </>
  );
}
