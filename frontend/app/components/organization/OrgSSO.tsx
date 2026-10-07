"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import type { SSOConfig } from "../../lib/api";
import {
  checkDomainAction,
  generateDomainAction,
  removeSSOAction,
  saveSSOAction,
} from "./actions";

// OrgSSO is the platform-admin surface for an org's SSO link to a Zitadel org.
// Beyond recording the Zitadel org id + email domain, it drives domain-ownership
// verification end to end: Zitadel's domain discovery only routes a customer's
// users to their IdP once their email domain is *verified*, so the operator
// publishes a DNS TXT record here and checks it — without leaving Quill. The
// verified flag itself lives in Zitadel (live-read); Quill only persists the
// generated challenge so the same record shows across page loads.
export function OrgSSO({ org, config }: { org: string; config: SSOConfig }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const [externalOrgId, setExternalOrgId] = useState(config.externalOrgId);
  const [emailDomain, setEmailDomain] = useState(config.emailDomain);

  function save(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSaved(false);
    startTransition(async () => {
      const res = await saveSSOAction(org, {
        externalOrgId: externalOrgId.trim(),
        emailDomain: emailDomain.trim(),
      });
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setSaved(true);
      router.refresh();
    });
  }

  function remove() {
    setError(null);
    setSaved(false);
    startTransition(async () => {
      const res = await removeSSOAction(org);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setExternalOrgId("");
      setEmailDomain("");
      router.refresh();
    });
  }

  return (
    <>
      <form className="org-sso-form" onSubmit={save}>
        {error && <div className="form-error">{error}</div>}
        {saved && <div className="banner">SSO link saved.</div>}

        <div className="subtle" style={{ marginBottom: "0.75rem" }}>
          Paste the customer&apos;s Zitadel organization id and their email domain.
          Users authenticating into that Zitadel org are mapped to this workspace
          and joined automatically. Domain discovery only routes them to their IdP
          once the email domain is verified below.
        </div>

        <label className="field">
          <span>
            Zitadel organization id{" "}
            <span className="subtle">— the org customer users authenticate against</span>
          </span>
          <input
            value={externalOrgId}
            onChange={(e) => setExternalOrgId(e.target.value)}
            placeholder="289734982374000123"
            autoComplete="off"
          />
        </label>

        <label className="field">
          <span>
            Email domain <span className="subtle">— drives Zitadel domain discovery</span>
          </span>
          <input
            value={emailDomain}
            onChange={(e) => setEmailDomain(e.target.value)}
            placeholder="company.com"
          />
        </label>

        <div className="form-actions">
          <button className="btn primary" type="submit" disabled={pending}>
            {pending ? "Saving…" : "Save SSO link"}
          </button>
          {config.configured && (
            <button
              type="button"
              className="btn danger"
              disabled={pending}
              onClick={remove}
            >
              Unlink
            </button>
          )}
        </div>
      </form>

      {config.configured && config.emailDomain !== "" && (
        <DomainVerification org={org} config={config} />
      )}
    </>
  );
}

// DomainVerification renders the domain-ownership status and the DNS challenge.
function DomainVerification({ org, config }: { org: string; config: SSOConfig }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);

  function generate() {
    setError(null);
    setNote(null);
    startTransition(async () => {
      const res = await generateDomainAction(org);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      router.refresh();
    });
  }

  function check() {
    setError(null);
    setNote(null);
    startTransition(async () => {
      const res = await checkDomainAction(org);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setNote(
        res.config.domainVerified
          ? "Domain verified — SSO logins will now route to this org."
          : "Checked. The domain still isn't verified.",
      );
      router.refresh();
    });
  }

  const hasRecord = config.domainRecordValue !== "";

  return (
    <div className="domain-verify" style={{ marginTop: "1rem" }}>
      <div className="settings-head" style={{ marginBottom: "0.5rem" }}>
        <h3 className="settings-title" style={{ fontSize: "0.95rem" }}>
          Domain verification <StatusBadge config={config} />
        </h3>
        <p className="subtle">
          Verify ownership of <span className="mono">{config.emailDomain}</span> so
          Zitadel routes its users to this org&apos;s identity provider.
        </p>
      </div>

      {error && <div className="form-error">{error}</div>}
      {note && <div className="banner">{note}</div>}

      {config.domainVerified ? (
        <div className="subtle">
          This domain is verified. Nothing more to do — logins for{" "}
          <span className="mono">{config.emailDomain}</span> are routed here.
        </div>
      ) : (
        <>
          {hasRecord ? (
            <>
              <p className="subtle" style={{ marginBottom: "0.4rem" }}>
                Add this DNS record at the customer&apos;s domain, then check
                verification. DNS can take time to propagate.
              </p>
              <dl className="dns-record mono">
                <div>
                  <dt className="subtle">Type</dt>
                  <dd>{(config.domainRecordType || "dns").toUpperCase() === "DNS" ? "TXT" : config.domainRecordType}</dd>
                </div>
                <div>
                  <dt className="subtle">Name</dt>
                  <dd>{config.domainRecordName}</dd>
                </div>
                <div>
                  <dt className="subtle">Value</dt>
                  <dd>{config.domainRecordValue}</dd>
                </div>
              </dl>
            </>
          ) : (
            <p className="subtle">
              Generate a DNS record for the customer to publish, then check
              verification.
            </p>
          )}

          <div className="form-actions" style={{ marginTop: "0.6rem" }}>
            <button className="btn" type="button" disabled={pending} onClick={generate}>
              {pending ? "Working…" : hasRecord ? "Regenerate record" : "Generate DNS record"}
            </button>
            {hasRecord && (
              <button
                className="btn primary"
                type="button"
                disabled={pending}
                onClick={check}
              >
                {pending ? "Checking…" : "Check verification"}
              </button>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// StatusBadge reflects the live-read verification state. "Unknown" means Quill
// couldn't reach the IdP to read the flag — distinct from a confirmed "pending".
function StatusBadge({ config }: { config: SSOConfig }) {
  if (!config.domainStatusKnown) {
    return <span className="badge">status unknown</span>;
  }
  if (config.domainVerified) {
    return <span className="badge green">verified</span>;
  }
  return <span className="badge amber">pending</span>;
}
