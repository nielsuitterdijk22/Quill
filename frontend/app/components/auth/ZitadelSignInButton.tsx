"use client";

// ZitadelSignInButton kicks off the Auth.js OIDC auth-code flow, redirecting the
// browser to Zitadel's hosted login and back to the app.
import { signIn } from "next-auth/react";

export function ZitadelSignInButton({ label }: { label: string }) {
  return (
    <div className="auth-page">
      <div className="ob-gh-connect">
        <h2 className="ob-gh-title">Welcome to Quill</h2>
        <p className="ob-gh-desc">Sign in with your organization account.</p>
        <button
          className="ob-btn-primary"
          onClick={() => signIn("zitadel", { callbackUrl: "/" })}
        >
          {label}
        </button>
      </div>
    </div>
  );
}
