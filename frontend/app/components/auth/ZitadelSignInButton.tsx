"use client";

// ZitadelSignInButton is Quill's sign-in launcher. It is a single button: sign-in
// always goes to the Zitadel instance, which handles account selection and — for
// SSO customers whose email domain is registered on their Zitadel org — routes
// them to their identity provider via Zitadel's own domain discovery. Quill does
// no per-org scoping; the org a user authenticates into is carried back on the
// token's resource-owner claim and mapped to their Quill workspace server-side.
import { useState } from "react";
import { signIn } from "next-auth/react";

import { AppTile } from "../icons/AppMarks";

// callbackUrl is where Auth.js returns after the OIDC round-trip. It defaults to
// the app root but is set to the invite path when a logged-out invitee is bounced
// here (see /invite/[token] -> /sign-in?redirect_url=...), so accepting an invite
// completes automatically instead of stranding them on the dashboard.
export function ZitadelSignInButton({ label, callbackUrl = "/" }: { label: string; callbackUrl?: string }) {
  const [pending, setPending] = useState(false);

  function startSignIn() {
    setPending(true);
    void signIn("zitadel", { callbackUrl });
  }

  return (
    <div className="auth-page">
      <div className="signin-card">
        <div className="auth-brand">
          <AppTile app="quill" size={28} /> Quill
        </div>
        <h1 className="signin-title">Welcome to Quill</h1>
        <p className="signin-sub">
          Continue to your workspace — for individuals and teams alike.
        </p>

        <button className="signin-btn" disabled={pending} onClick={startSignIn} type="button">
          {pending ? "Redirecting…" : label}
        </button>

        <p className="signin-foot">
          Secured by single sign-on. Organizations with SSO are routed to their
          provider automatically.
        </p>
      </div>
    </div>
  );
}
