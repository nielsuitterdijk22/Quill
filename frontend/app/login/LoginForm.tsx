"use client";

import Link from "next/link";
import { useFormState, useFormStatus } from "react-dom";

import { loginAction, type AuthFormState } from "./actions";

const initialState: AuthFormState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="auth-submit" type="submit" disabled={pending}>
      {pending ? "Signing in…" : "Sign in"}
    </button>
  );
}

export function LoginForm() {
  const [state, formAction] = useFormState(loginAction, initialState);

  return (
    <div className="center-wrap">
      <div className="landing-pitch">
        <div className="auth-brand landing-brand">
          <span className="dot" /> Quill
        </div>
        <p className="landing-desc">
          A self-hosted code platform built for teams who own their data. Git
          hosting, pull requests, branch policies, and CI pipelines — no cloud
          lock-in, no telemetry, no vendor fees.
        </p>
        <p className="landing-desc">
          Deploy on any server. Your code stays yours.
        </p>
        <Link className="btn primary landing-cta" href="/register">
          Get started — it&apos;s free
        </Link>
      </div>

      <div className="auth-card">
        <p className="auth-tagline">Sign in to your instance</p>

        {state.error && <div className="auth-error">{state.error}</div>}

        <form action={formAction}>
          <label className="field">
            <span>Username</span>
            <input
              name="username"
              autoComplete="username"
              autoFocus
              required
            />
          </label>
          <label className="field">
            <span>Password</span>
            <input
              name="password"
              type="password"
              autoComplete="current-password"
              required
            />
          </label>
          <SubmitButton />
        </form>

        <p className="auth-hint">
          No account? <Link href="/register">Register</Link>. The first account
          becomes the admin.
        </p>
      </div>
    </div>
  );
}
