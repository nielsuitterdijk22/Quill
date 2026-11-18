"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { AppTile } from "../icons/AppMarks";

type State = "working" | "ok" | "failed";

export function VerifyEmail({ userID, code }: { userID: string; code: string }) {
  const [state, setState] = useState<State>("working");
  const [message, setMessage] = useState("");

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/backend/auth/verify-email", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ userId: userID, code }),
        });
        if (cancelled) return;
        if (res.ok) {
          setState("ok");
          return;
        }
        const body = (await res.json().catch(() => null)) as { message?: string } | null;
        setMessage(body?.message ?? "This verification link is invalid or has expired.");
        setState("failed");
      } catch {
        if (cancelled) return;
        setMessage("Couldn't reach Quill. Try the link again.");
        setState("failed");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [userID, code]);

  return (
    <div className="auth-page">
      <div className="signin-card">
        <div className="auth-brand">
          <AppTile app="quill" size={28} /> Quill
        </div>
        {state === "working" && <h1 className="signin-title">Verifying your email…</h1>}
        {state === "ok" && (
          <>
            <h1 className="signin-title">Email verified</h1>
            <p className="signin-sub">Your account is ready. Sign in with your username and password.</p>
            <Link className="signin-btn signin-btn-link" href="/sign-in">
              Continue to sign in
            </Link>
          </>
        )}
        {state === "failed" && (
          <>
            <h1 className="signin-title">Couldn&apos;t verify</h1>
            <div className="auth-error signup-error">{message}</div>
            <Link className="signin-btn signin-btn-link" href="/sign-in">
              Go to sign in
            </Link>
          </>
        )}
      </div>
    </div>
  );
}
