"use client";

import Link from "next/link";
import { useFormState, useFormStatus } from "react-dom";

import type { Branch } from "../../../../../../../lib/api";
import { createPullAction, type CreatePullState } from "./actions";

const initialState: CreatePullState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Creating…" : "Create pull request"}
    </button>
  );
}

// NewPullForm collects the branches, title, and description for a new PR. The
// org/repo slugs are bound into the server action.
export function NewPullForm({
  org,
  repo,
  branches,
  defaultBranch,
}: {
  org: string;
  repo: string;
  branches: Branch[];
  defaultBranch: string;
}) {
  const action = createPullAction.bind(null, org, repo);
  const [state, formAction] = useFormState(action, initialState);

  const names = branches.map((b) => b.name);
  const defaultHead = names.find((n) => n !== defaultBranch) ?? "";

  return (
    <div className="panel form-narrow">
      <h2>Open a pull request</h2>
      <div className="readme-body">
        {state.error && <div className="form-error">{state.error}</div>}
        {branches.length < 2 && (
          <div className="banner">
            This repository has fewer than two branches. Push another branch to
            open a pull request.
          </div>
        )}
        <form action={formAction}>
          <div className="branch-row">
            <label className="field">
              <span>Base (merge into)</span>
              <select name="base" defaultValue={defaultBranch}>
                {names.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
            <span className="branch-arrow">←</span>
            <label className="field">
              <span>Compare (merge from)</span>
              <select name="head" defaultValue={defaultHead}>
                {names.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <label className="field">
            <span>Title</span>
            <input
              name="title"
              autoFocus
              required
              placeholder="Short summary"
            />
          </label>
          <label className="field">
            <span>Description</span>
            <textarea
              name="body"
              rows={5}
              placeholder="Explain the change (optional)"
            />
          </label>
          <div className="form-actions">
            <SubmitButton />
            <Link
              className="btn ghost"
              href={`/orgs/${org}/repos/${repo}/pulls`}
            >
              Cancel
            </Link>
          </div>
        </form>
      </div>
    </div>
  );
}
