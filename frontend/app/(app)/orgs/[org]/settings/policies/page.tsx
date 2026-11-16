import { getTenantEgress, getTenantEnvironmentPolicies, getTenantPolicies } from "../../../../../lib/api";
import { PolicyManager } from "../../../../../components/policy/PolicyManager";
import { EnvironmentPolicyManager } from "../../../../../components/policy/EnvironmentPolicyManager";
import { TenantEgressForm } from "../../../../../components/egress/EgressForms";
import { loadOrg } from "../org";

export default async function OrgPoliciesSettings({ params }: { params: { org: string } }) {
  const { token, org, canEdit } = await loadOrg(params.org);
  const [branchRes, envRes, egress] = await Promise.all([
    getTenantPolicies(token, params.org),
    getTenantEnvironmentPolicies(token, params.org),
    getTenantEgress(token, params.org),
  ]);
  return (
    <>
      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Branch policies</h2>
          <p className="subtle">
            Rules for every repository in every project of this organization. Projects and repositories may add
            stricter rules but cannot weaken these. Lock a rule to forbid loosening it.
          </p>
        </div>
        <PolicyManager
          target={{ scope: "tenant", tenant: org.slug }}
          policies={branchRes.ok ? branchRes.data.policies : []}
          canLock
          canEdit={canEdit}
        />
      </section>
      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Environment policies</h2>
          <p className="subtle">
            Gate deploys for every repository in this organization. Projects and repositories may add stricter
            gates but cannot weaken these.
          </p>
        </div>
        <EnvironmentPolicyManager
          target={{ scope: "tenant", tenant: org.slug }}
          policies={envRes.ok ? envRes.data.policies : []}
          canLock
          canEdit={canEdit}
        />
      </section>
      {egress && (
        <section className="settings-section settings-card" id="egress">
          <div className="settings-head">
            <h2 className="settings-title">Pipeline egress</h2>
            <p className="subtle">
              Which hosts pipelines may reach. Every repository in this organization inherits this policy and can
              add hosts of its own.
            </p>
          </div>
          <TenantEgressForm policy={egress} tenant={org.slug} path={`/orgs/${org.slug}/settings/policies`} />
        </section>
      )}
    </>
  );
}
