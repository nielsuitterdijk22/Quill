"use client";

// ZitadelSignInButton is Quill's sign-in launcher: a clean branded card whose
// button starts the Auth.js OIDC auth-code flow, redirecting to the hosted login
// and back. Works the same for individuals and team members — one account.
import { useState } from "react";
import { signIn } from "next-auth/react";

export function ZitadelSignInButton({ label }: { label: string }) {
  const [pending, setPending] = useState(false);

  return (
    <div className="auth-page">
      <div className="signin-card">
        <div className="auth-brand">
          <span className="dot" /> Quill
        </div>
        <h1 className="signin-title">Welcome to Quill</h1>
        <p className="signin-sub">
          Continue to your workspace — for individuals and teams alike.
        </p>
        <button
          className="signin-btn"
          disabled={pending}
          onClick={() => {
            setPending(true);
            void signIn("zitadel", { callbackUrl: "/" });
          }}
        >
          {pending ? "Redirecting…" : label}
        </button>
        <p className="signin-foot">Secured by your organization&apos;s single sign-on.</p>
      </div>
    </div>
  );
}
