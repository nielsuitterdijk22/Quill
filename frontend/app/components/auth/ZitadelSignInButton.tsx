"use client";

// ZitadelSignInButton is Quill's sign-in launcher. By default (auto) it starts
// the Auth.js OIDC flow the moment it renders, so people go straight to the
// login without an extra "Sign in" click; a short branded screen covers the
// redirect, and a button appears if it takes unusually long. With auto off —
// after an Auth.js error, so a failing login can never loop — it shows the
// card with the error and a button. Works the same for individuals and team
// members: one account.
import { useEffect, useRef, useState } from "react";
import { signIn } from "next-auth/react";

import { AppTile } from "../icons/AppMarks";

type Props = {
  label: string;
  // Start the flow on render (default true).
  auto?: boolean;
  // Open Zitadel's registration form instead of its login form.
  register?: boolean;
  // Shown above the button: why the user is here (expired session, error…).
  notice?: string;
};

export function ZitadelSignInButton({ label, auto = true, register = false, notice }: Props) {
  const [pending, setPending] = useState(auto);
  const [slow, setSlow] = useState(false);
  const started = useRef(false);

  function start() {
    setPending(true);
    void signIn("zitadel", { callbackUrl: "/" }, register ? { prompt: "create" } : undefined);
  }

  useEffect(() => {
    if (!auto || started.current) return; // StrictMode runs effects twice in dev
    started.current = true;
    start();
    const t = setTimeout(() => setSlow(true), 6000);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="auth-page">
      <div className="signin-card">
        <div className="auth-brand">
          <AppTile app="quill" size={28} /> Quill
        </div>
        {pending && !slow ? (
          <>
            <div className="signin-spinner" role="status" aria-label="Redirecting to sign in" />
            <p className="signin-sub">{notice ?? "Taking you to sign in…"}</p>
          </>
        ) : (
          <>
            <h1 className="signin-title">Welcome to Quill</h1>
            <p className="signin-sub">
              {notice ?? "Continue to your workspace — for individuals and teams alike."}
            </p>
            <button className="signin-btn" disabled={pending && !slow} onClick={start}>
              {label}
            </button>
            <p className="signin-foot">Secured by your organization&apos;s single sign-on.</p>
          </>
        )}
      </div>
    </div>
  );
}
