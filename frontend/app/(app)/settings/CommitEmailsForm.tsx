"use client";

import { useFormState, useFormStatus } from "react-dom";

import { saveCommitEmailsAction, type CommitEmailsFormState } from "./actions";

const initial: CommitEmailsFormState = {};

function SaveButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Saving…" : "Save commit emails"}
    </button>
  );
}

// CommitEmailsForm manages the extra addresses the contribution graph counts
// commits under (the account email always counts).
export function CommitEmailsForm({ primary, emails }: { primary: string; emails: string[] }) {
  const [state, formAction] = useFormState(saveCommitEmailsAction, initial);

  return (
    <div className="panel form-narrow" id="commit-emails">
      <h2>Commit emails</h2>
      <div className="readme-body">
        <p className="hint">
          Commits authored with these addresses count on your contribution graph, along with{" "}
          <b>{primary}</b>. Only repositories you can read are counted.
        </p>
        {state.error && <div className="form-error">{state.error}</div>}
        {state.ok && <div className="form-success">Commit emails saved.</div>}
        <form action={formAction}>
          <label className="field">
            <span>Extra emails, one per line</span>
            <textarea
              name="emails"
              rows={3}
              defaultValue={emails.join("\n")}
              placeholder="you@work.example"
              spellCheck={false}
            />
          </label>
          <div className="form-actions">
            <SaveButton />
          </div>
        </form>
      </div>
    </div>
  );
}
