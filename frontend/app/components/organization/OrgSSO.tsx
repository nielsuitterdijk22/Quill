"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import type { SSOConfig } from "../../lib/api";
import { removeSSOAction, saveSSOAction, verifySSODomainAction } from "./actions";

function CopyField({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <>
      <span className="clone-label">{label}</span>
      <div className="clone-field">
        <input readOnly value={value} aria-label={label} />
        <button
          type="button"
          className="btn ghost copy-btn"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(value);
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            } catch {
              /* clipboard unavailable */
            }
          }}
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
    </>
  );
}

// OrgSSO is the admin surface for an organization's single sign-on: the
// company identity provider (OIDC or SAML), the email domain whose sign-ins it
// takes over, and the DNS proof that the organization owns that domain. The
// client secret is write-only: it is never sent back, and leaving the field
// blank keeps the stored one.
export function OrgSSO({ org, config }: { org: string; config: SSOConfig }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const [protocol, setProtocol] = useState(config.protocol || "oidc");
  const [issuer, setIssuer] = useState(config.issuer);
  const [clientId, setClientId] = useState(config.clientId);
  const [clientSecret, setClientSecret] = useState("");
  const [emailDomain, setEmailDomain] = useState(config.emailDomain);
  const [enabled, setEnabled] = useState(config.enabled);

  function run(fn: () => Promise<{ ok: true } | { ok: false; error: string }>, done: string) {
    setError(null);
    setNotice(null);
    startTransition(async () => {
      const res = await fn();
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setNotice(done);
      router.refresh();
    });
  }

  function save(e: React.FormEvent) {
    e.preventDefault();
    run(async () => {
      const res = await saveSSOAction(org, { protocol, issuer, clientId, clientSecret, emailDomain, enabled });
      if (res.ok) setClientSecret("");
      return res;
    }, "SSO settings saved.");
  }

  const status = !config.configured
    ? null
    : !config.enabled
      ? { cls: "", text: "Off" }
      : config.lastError
        ? { cls: "red", text: "Needs attention" }
        : config.routing
          ? { cls: "green", text: `Active for @${config.emailDomain}` }
          : config.active
            ? { cls: "amber", text: "Active — verify the domain to route sign-ins" }
            : { cls: "amber", text: "Pending" };

  return (
    <div>
      {config.available === false && (
        <div className="banner">Single sign-on login isn&rsquo;t available on this server yet; settings are stored only.</div>
      )}
      {config.planAllowsSso === false && (
        <div className="banner">Single sign-on is part of the Team plan.</div>
      )}
      {status && (
        <p>
          Status: <span className={`badge ${status.cls}`}>{status.text}</span>
        </p>
      )}
      {config.lastError && <div className="form-error">Couldn&rsquo;t apply: {config.lastError}. Save again to retry.</div>}

      <form className="org-sso-form" onSubmit={save}>
        {error && <div className="form-error">{error}</div>}
        {notice && <div className="banner">{notice}</div>}

        <label className="check">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          <span>Enable SSO for this organization</span>
        </label>

        <label className="field">
          <span>Protocol</span>
          <select value={protocol} onChange={(e) => setProtocol(e.target.value)}>
            <option value="oidc">OIDC</option>
            <option value="saml">SAML</option>
          </select>
        </label>

        <label className="field">
          <span>{protocol === "saml" ? "Metadata URL" : "Issuer URL"}</span>
          <input
            value={issuer}
            onChange={(e) => setIssuer(e.target.value)}
            placeholder={protocol === "saml" ? "https://idp.company.com/metadata.xml" : "https://login.company.com"}
          />
        </label>

        {protocol === "oidc" && (
          <>
            <label className="field">
              <span>Client ID</span>
              <input value={clientId} onChange={(e) => setClientId(e.target.value)} />
            </label>
            <label className="field">
              <span>
                Client secret{" "}
                {config.hasSecret && <span className="subtle">— leave blank to keep the stored secret</span>}
              </span>
              <input
                type="password"
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
                placeholder={config.hasSecret ? "••••••••" : ""}
                autoComplete="new-password"
              />
            </label>
          </>
        )}

        <label className="field">
          <span>
            Email domain <span className="subtle">— sign-ins from this domain go to your identity provider</span>
          </span>
          <input value={emailDomain} onChange={(e) => setEmailDomain(e.target.value)} placeholder="company.com" />
        </label>

        <div className="form-actions">
          <button className="btn primary" type="submit" disabled={pending}>
            {pending ? "Saving…" : "Save SSO settings"}
          </button>
          {config.configured && (
            <button type="button" className="btn danger" disabled={pending} onClick={() => run(() => removeSSOAction(org), "SSO removed.")}>
              Remove
            </button>
          )}
        </div>
      </form>

      {config.configured && (
        <div className="token-reveal">
          <p className="hint">Register Quill at your identity provider:</p>
          {config.protocol === "oidc" && config.redirectUri && (
            <CopyField label="Redirect (callback) URL" value={config.redirectUri} />
          )}
          {config.protocol === "saml" && config.samlAcsUrl && (
            <>
              <CopyField label="ACS URL" value={config.samlAcsUrl} />
              <CopyField label="Entity ID / SP metadata URL" value={config.samlEntityId ?? ""} />
            </>
          )}
          {config.protocol === "saml" && !config.samlAcsUrl && (
            <p className="subtle">Save with SSO enabled to get the ACS URL and entity ID.</p>
          )}
        </div>
      )}

      {!(config.configured && config.emailDomain) && (
        <div className="token-reveal">
          <p className="hint">
            <b>Verify your email domain</b> — enter it above (e.g. company.com) and save. You&rsquo;ll get a DNS TXT
            record to publish; once it&rsquo;s verified, sign-ins from that domain go to your identity provider.
          </p>
        </div>
      )}

      {config.configured && config.emailDomain && (
        <div className="token-reveal">
          <p className="hint">
            <b>Verify your email domain</b>
          </p>
          {config.domainVerified ? (
            <p className="hint">
              <span className="badge green">Verified</span> @{config.emailDomain} belongs to this organization.
            </p>
          ) : (
            <>
              <p className="hint">
                Prove you own {config.emailDomain}: add this DNS TXT record, then verify. Until then, sign-ins from
                @{config.emailDomain} aren&rsquo;t routed to your identity provider.
              </p>
              {config.verifyRecordName && <CopyField label="Record name" value={config.verifyRecordName} />}
              {config.verifyRecordValue && <CopyField label="Record value" value={config.verifyRecordValue} />}
              <button
                type="button"
                className="btn"
                disabled={pending}
                onClick={() => run(() => verifySSODomainAction(org), "Domain verified.")}
              >
                Verify domain
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
