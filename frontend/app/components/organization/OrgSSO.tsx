"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import type { SSOConfig } from "../../lib/api";
import { removeSSOAction, saveSSOAction } from "./actions";

// OrgSSO is the admin form for an organization's SSO configuration. The client
// secret is write-only: it is never sent back, and leaving the field blank keeps
// the stored one. Consuming this config to route logins is an auth-layer
// follow-up; saving here only stores the settings.
export function OrgSSO({ org, config }: { org: string; config: SSOConfig }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const [protocol, setProtocol] = useState(config.protocol || "oidc");
  const [issuer, setIssuer] = useState(config.issuer);
  const [clientId, setClientId] = useState(config.clientId);
  const [clientSecret, setClientSecret] = useState("");
  const [emailDomain, setEmailDomain] = useState(config.emailDomain);
  const [enabled, setEnabled] = useState(config.enabled);

  function save(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSaved(false);
    startTransition(async () => {
      const res = await saveSSOAction(org, {
        protocol,
        issuer,
        clientId,
        clientSecret,
        emailDomain,
        enabled,
      });
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setClientSecret("");
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
      router.refresh();
    });
  }

  return (
    <form className="org-sso-form" onSubmit={save}>
      {error && <div className="form-error">{error}</div>}
      {saved && <div className="banner">SSO settings saved.</div>}

      <label className="check">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
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
          placeholder="https://idp.company.com"
        />
      </label>

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

      <label className="field">
        <span>Email domain <span className="subtle">— routes these users to this org</span></span>
        <input
          value={emailDomain}
          onChange={(e) => setEmailDomain(e.target.value)}
          placeholder="company.com"
        />
      </label>

      <div className="form-actions">
        <button className="btn primary" type="submit" disabled={pending}>
          {pending ? "Saving…" : "Save SSO settings"}
        </button>
        {config.configured && (
          <button
            type="button"
            className="btn danger"
            disabled={pending}
            onClick={remove}
          >
            Remove
          </button>
        )}
      </div>
    </form>
  );
}
