"use client";

import { useEffect, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import { provisionSSOTenantAction } from "./actions";

// NewOrgForm onboards an SSO customer from a modal: it creates the Quill tenant
// and provisions a backing Zitadel org (external IdP with auto-linking, ORG_OWNER
// admin shells, external-only login) in one action. The admins get NO email — they
// sign in through the customer's IdP and Zitadel auto-links them to the pre-created
// shells by verified email, landing as tenant admins on first login.
export function NewOrgForm() {
  const router = useRouter();
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [open, setOpen] = useState(false);
  const [pending, startTransition] = useTransition();

  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [protocol, setProtocol] = useState("oidc");
  const [issuer, setIssuer] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [emailDomain, setEmailDomain] = useState("");
  const [admins, setAdmins] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<{ slug: string; externalOrgId: string; adminEmails: string[] } | null>(null);

  useEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
    if (open) el.showModal();
    else el.close();
  }, [open]);

  function close() {
    setOpen(false);
    setError(null);
    setDone(null);
  }

  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!slug.trim()) {
      setError("Slug is required.");
      return;
    }
    if (!issuer.trim()) {
      setError(protocol === "saml" ? "Metadata URL is required." : "Issuer URL is required.");
      return;
    }
    setError(null);
    const adminEmails = admins.split(/[\s,;]+/).map((s) => s.trim()).filter(Boolean);
    startTransition(async () => {
      const res = await provisionSSOTenantAction({
        slug,
        name,
        protocol,
        issuer,
        clientId,
        clientSecret,
        emailDomain,
        adminEmails,
      });
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setDone({ slug: res.slug, externalOrgId: res.externalOrgId, adminEmails });
    });
  }

  return (
    <>
      <button type="button" className="btn primary" onClick={() => setOpen(true)}>
        Provision SSO organization
      </button>

      <dialog ref={dialogRef} className="provision-dialog" onClose={close}>
        {done ? (
          <div className="org-created">
            <h2>Provisioned</h2>
            <div className="banner">
              <strong>{done.slug}</strong> — Zitadel org{" "}
              <span className="mono">{done.externalOrgId}</span> created and linked.
            </div>
            {done.adminEmails.length > 0 && (
              <p className="subtle">
                {done.adminEmails.join(", ")} {done.adminEmails.length === 1 ? "was" : "were"} pre-created
                as ORG_OWNER. They receive no email — on first sign-in through the IdP they auto-link and
                land as tenant admins.
              </p>
            )}
            <div className="form-actions">
              <button type="button" className="pill" onClick={close}>
                Close
              </button>
              <button
                className="btn primary"
                type="button"
                onClick={() => router.push(`/admin/organizations/${done.slug}`)}
              >
                Go to organization →
              </button>
            </div>
          </div>
        ) : (
          <form className="org-sso-form" onSubmit={submit}>
            <h2>Provision SSO organization</h2>
            {error && <div className="form-error">{error}</div>}

            <label className="field">
              <span>Slug <span className="subtle">— becomes part of every repository URL</span></span>
              <input
                value={slug}
                onChange={(e) => setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""))}
                placeholder="acme"
                autoFocus
              />
            </label>
            <label className="field">
              <span>Display name <span className="subtle">— optional</span></span>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme Inc." />
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
              <input value={issuer} onChange={(e) => setIssuer(e.target.value)} placeholder="https://acme.okta.com" />
            </label>
            {protocol === "oidc" && (
              <>
                <label className="field">
                  <span>Client ID</span>
                  <input value={clientId} onChange={(e) => setClientId(e.target.value)} />
                </label>
                <label className="field">
                  <span>Client secret</span>
                  <input
                    type="password"
                    value={clientSecret}
                    onChange={(e) => setClientSecret(e.target.value)}
                    autoComplete="new-password"
                  />
                </label>
              </>
            )}
            <label className="field">
              <span>Email domain <span className="subtle">— for domain-discovery routing (verify in Zitadel)</span></span>
              <input value={emailDomain} onChange={(e) => setEmailDomain(e.target.value)} placeholder="acme.com" />
            </label>
            <label className="field">
              <span>Admin emails <span className="subtle">— pre-created as ORG_OWNER (comma or newline separated)</span></span>
              <textarea
                value={admins}
                onChange={(e) => setAdmins(e.target.value)}
                placeholder="alice@acme.com, bob@acme.com"
                rows={3}
              />
            </label>

            <div className="form-actions">
              <button type="button" className="pill" onClick={close} disabled={pending}>
                Cancel
              </button>
              <button className="btn primary" type="submit" disabled={pending}>
                {pending ? "Provisioning…" : "Provision"}
              </button>
            </div>
          </form>
        )}
      </dialog>
    </>
  );
}
