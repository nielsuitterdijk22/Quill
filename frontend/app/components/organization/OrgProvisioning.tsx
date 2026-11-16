"use client";

import { useState, useTransition } from "react";

import type { SCIMToken } from "../../lib/api";
import { createSCIMTokenAction, revokeSCIMTokenAction } from "./actions";

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      className="btn ghost copy-btn"
      aria-label={label}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        } catch {
          /* clipboard unavailable; ignore */
        }
      }}
    >
      {copied ? "Copied" : "Copy"}
    </button>
  );
}

function formatDate(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

// OrgProvisioning manages the organization's SCIM 2.0 provisioning tokens: the
// identity provider (Okta, Entra ID, …) uses the base URL plus a token to
// create, update and deprovision members and groups. A token is shown once.
export function OrgProvisioning({
  org,
  baseUrl,
  tokens,
  unavailable,
}: {
  org: string;
  baseUrl: string;
  tokens: SCIMToken[];
  unavailable?: string;
}) {
  const [pending, startTransition] = useTransition();
  const [name, setName] = useState("");
  const [created, setCreated] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  function create() {
    setError(null);
    startTransition(async () => {
      const res = await createSCIMTokenAction(org, name || "SCIM");
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setCreated(res.token);
      setName("");
    });
  }

  function revoke(id: string) {
    setError(null);
    startTransition(async () => {
      const res = await revokeSCIMTokenAction(org, id);
      if (!res.ok) setError(res.error);
    });
  }

  return (
    <div>
      {unavailable && <div className="banner">{unavailable}</div>}
      {error && <div className="form-error">{error}</div>}

      <span className="clone-label">SCIM base URL</span>
      <div className="clone-field">
        <input readOnly value={baseUrl} aria-label="SCIM base URL" />
        <CopyButton value={baseUrl} label="Copy SCIM base URL" />
      </div>

      {created && (
        <div className="token-reveal">
          <p className="hint">Copy this token into your identity provider now &mdash; it won&rsquo;t be shown again.</p>
          <span className="clone-label">Bearer token</span>
          <div className="clone-field">
            <input readOnly value={created} aria-label="SCIM bearer token" />
            <CopyButton value={created} label="Copy SCIM token" />
          </div>
        </div>
      )}

      <div className="token-create">
        <input
          type="text"
          className="token-name-input"
          placeholder="Token name (e.g. Okta)"
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-label="Token name"
          maxLength={100}
        />
        <button type="button" className="btn primary" onClick={create} disabled={pending}>
          {pending ? "Creating…" : "Create token"}
        </button>
      </div>

      {tokens.length > 0 && (
        <ul className="token-list">
          {tokens.map((t) => (
            <li key={t.id} className="token-row">
              <div className="token-meta">
                <span className="token-name">{t.name}</span>
                <span className="subtle">
                  Created {formatDate(t.createdAt)}
                  {t.lastUsedAt ? ` · last used ${formatDate(t.lastUsedAt)}` : " · never used"}
                </span>
              </div>
              <button type="button" className="btn ghost danger" onClick={() => revoke(t.id)} disabled={pending}>
                Revoke
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
