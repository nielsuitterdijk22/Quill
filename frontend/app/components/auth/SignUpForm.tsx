"use client";

// SignUpForm is Quill's account creation: a handle, an email and a password —
// like GitHub, no first/last name. Organizations are created after signing in.
// The backend creates the account in Zitadel (which emails a verification
// code); sign-in itself stays on Zitadel's hosted pages.
import Link from "next/link";
import { useState } from "react";

import { isValidHandle } from "../../lib/slug";
import { AppTile } from "../icons/AppMarks";

export function SignUpForm() {
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [done, setDone] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!isValidHandle(username)) {
      setError("Username: lowercase letters, digits, '.', '_' or '-', starting with a letter or digit.");
      return;
    }
    setPending(true);
    try {
      const res = await fetch("/api/backend/auth/signup", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, email: email.trim(), password }),
      });
      if (res.ok) {
        setDone(true);
        return;
      }
      const body = (await res.json().catch(() => null)) as { message?: string } | null;
      setError(body?.message ?? "Could not create your account. Try again.");
    } catch {
      setError("Couldn't reach Quill. Try again.");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="auth-page">
      <div className="signin-card">
        <div className="auth-brand">
          <AppTile app="quill" size={28} /> Quill
        </div>
        {done ? (
          <>
            <h1 className="signin-title">Check your email</h1>
            <p className="signin-sub">
              We sent a verification code to <b>{email}</b>. Sign in with your username{" "}
              <code>{username}</code> and your password, then enter the code when asked.
            </p>
            <Link className="signin-btn signin-btn-link" href="/sign-in">
              Continue to sign in
            </Link>
          </>
        ) : (
          <>
            <h1 className="signin-title">Create your account</h1>
            <p className="signin-sub">One account for personal projects and teams. Organizations come next.</p>
            {error && <div className="auth-error signup-error">{error}</div>}
            <form className="signup-form" onSubmit={submit}>
              <label className="field">
                <span>Username</span>
                <input
                  value={username}
                  onChange={(e) => setUsername(e.target.value.toLowerCase().replace(/[^a-z0-9._-]/g, ""))}
                  autoComplete="username"
                  maxLength={63}
                  required
                  autoFocus
                  placeholder="your-handle"
                />
              </label>
              {username && (
                <p className="hint">
                  Your repositories will live at <code>{username}/repo-name</code>
                </p>
              )}
              <label className="field">
                <span>Email</span>
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  autoComplete="email"
                  required
                  placeholder="you@example.com"
                />
              </label>
              <label className="field">
                <span>Password</span>
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  minLength={8}
                  maxLength={128}
                  required
                />
              </label>
              <button className="signin-btn" type="submit" disabled={pending}>
                {pending ? "Creating…" : "Create account"}
              </button>
            </form>
            <p className="signin-foot">
              Already have an account? <Link href="/sign-in">Sign in</Link>
            </p>
          </>
        )}
      </div>
    </div>
  );
}
