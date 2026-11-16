import { notFound } from "next/navigation";

import { getOrgSCIMTokens, getOrgSSO, getOrgBilling, type SSOConfig } from "../../../../../lib/api";
import { OrgSSO } from "../../../../../components/organization/OrgSSO";
import { OrgProvisioning } from "../../../../../components/organization/OrgProvisioning";
import { loadOrg } from "../org";

const EMPTY_SSO: SSOConfig = {
  configured: false, protocol: "oidc", issuer: "", clientId: "", emailDomain: "", enabled: false, hasSecret: false, updatedAt: "",
};

// Sign-in & provisioning: the company identity provider, in the two roles it
// plays — SSO decides how people sign in; SCIM decides who is in the org.
export default async function OrgSignInSettings({ params }: { params: { org: string } }) {
  const { token, org, canEdit } = await loadOrg(params.org);
  if (!canEdit) notFound();
  const [ssoRes, scimRes, billingRes] = await Promise.all([
    getOrgSSO(token, params.org),
    getOrgSCIMTokens(token, params.org),
    getOrgBilling(token, params.org),
  ]);
  const sso = ssoRes.ok ? ssoRes.data : EMPTY_SSO;
  const locked = billingRes.ok && billingRes.data.locked;
  return (
    <>
      <section className="settings-section settings-card">
        <div className="settings-head">
          <h2 className="settings-title">Your company identity provider</h2>
          <p className="subtle">
            Connect Okta, Microsoft Entra ID, Google Workspace or any OIDC/SAML provider. It plays two separate
            roles — most companies set up both:
          </p>
        </div>
        <ol className="settings-steps">
          <li>
            <b>Single sign-on</b> — <i>how people sign in.</i> Anyone signing in with an address on your verified
            email domain goes straight to your company login and joins {org.name}.
          </li>
          <li>
            <b>Provisioning (SCIM)</b> — <i>who is in the organization.</i> Your identity provider adds the people
            you assign, removes the ones you deactivate (they lose access immediately) and keeps your groups in
            sync. It works per organization: the token below only ever touches {org.name}.
          </li>
        </ol>
      </section>

      <section className="settings-section settings-card" id="sso">
        <div className="settings-head">
          <h2 className="settings-title">1 · Single sign-on</h2>
          <p className="subtle">The client secret is stored encrypted and never shown again.</p>
        </div>
        <OrgSSO org={org.slug} config={sso} />
      </section>

      <section className="settings-section settings-card" id="scim">
        <div className="settings-head">
          <h2 className="settings-title">2 · Provisioning (SCIM)</h2>
          <p className="subtle">
            In your identity provider, add a SCIM 2.0 app with the base URL and a bearer token from here. People are
            matched to Quill accounts by email; groups appear under Groups.
          </p>
        </div>
        <OrgProvisioning
          org={org.slug}
          baseUrl={scimRes.ok ? scimRes.data.baseUrl : ""}
          tokens={scimRes.ok ? scimRes.data.tokens : []}
          unavailable={locked ? "Provisioning new members is paused until this organization subscribes; deprovisioning keeps working." : undefined}
        />
      </section>
    </>
  );
}
