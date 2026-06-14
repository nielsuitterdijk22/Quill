"use client";

import Link from "next/link";
import { useFormState, useFormStatus } from "react-dom";

import { registerAction, type AuthFormState } from "./actions";

const initialState: AuthFormState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="auth-submit" type="submit" disabled={pending}>
      {pending ? "Creating account…" : "Create account"}
    </button>
  );
}

export function RegisterForm() {
  const [state, formAction] = useFormState(registerAction, initialState);

  return (
    <div className="center-wrap">
      <div className="auth-card">
        <div className="auth-brand">
          <span className="dot" /> Quill
        </div>
        <p className="auth-tagline">Create your account.</p>

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
            <span>Email</span>
            <input name="email" type="email" autoComplete="email" required />
          </label>
          <label className="field">
            <span>Display name (optional)</span>
            <input name="displayName" autoComplete="name" />
          </label>
          <label className="field">
            <span>Password</span>
            <input
              name="password"
              type="password"
              autoComplete="new-password"
              minLength={8}
              required
            />
          </label>
          <SubmitButton />
        </form>

        <p className="auth-hint">
          Already have an account? <Link href="/login">Sign in</Link>.
        </p>
      </div>
    </div>
  );
}
