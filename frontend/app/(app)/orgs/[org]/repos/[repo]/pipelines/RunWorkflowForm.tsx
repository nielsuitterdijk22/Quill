"use client";

import { useFormState, useFormStatus } from "react-dom";

import type { Branch, PipelineSummary } from "../../../../../../lib/api";
import { triggerRunAction, type TriggerState } from "./actions";

const initialState: TriggerState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Starting…" : "Run workflow"}
    </button>
  );
}

// RunWorkflowForm lets a user trigger a workflow manually on a chosen ref. The
// org/repo slugs are bound into the server action.
export function RunWorkflowForm({
  org,
  repo,
  pipelines,
  branches,
  defaultBranch,
}: {
  org: string;
  repo: string;
  pipelines: PipelineSummary[];
  branches: Branch[];
  defaultBranch: string;
}) {
  const action = triggerRunAction.bind(null, org, repo);
  const [state, formAction] = useFormState(action, initialState);

  if (pipelines.length === 0) return null;

  return (
    <div className="panel run-action-panel">
      <div className="run-action-copy">
        <strong>Run workflow</strong>
        <span className="sub">
          Start a workflow manually against a branch in this repository.
        </span>
      </div>
      <form className="run-workflow-form" action={formAction}>
        <label>
          <span>Workflow</span>
          <select name="workflow" defaultValue={pipelines[0].workflowPath}>
            {pipelines.map((p) => (
              <option key={p.workflowPath} value={p.workflowPath}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Ref</span>
          <select name="ref" defaultValue={defaultBranch}>
            {branches.map((b) => (
              <option key={b.name} value={b.name}>
                {b.name}
              </option>
            ))}
          </select>
        </label>
        <SubmitButton />
      </form>
      {state.error && <div className="form-error">{state.error}</div>}
    </div>
  );
}
