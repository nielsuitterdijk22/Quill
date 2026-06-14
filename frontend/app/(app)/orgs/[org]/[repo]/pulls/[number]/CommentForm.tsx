"use client";

import { useEffect, useRef } from "react";
import { useFormState, useFormStatus } from "react-dom";

import { addCommentAction, type CommentState } from "./actions";

const initialState: CommentState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Commenting…" : "Comment"}
    </button>
  );
}

// CommentForm adds a comment to the PR conversation. On success the page
// revalidates server-side; we reset the textarea so it's ready for the next one.
export function CommentForm({
  org,
  repo,
  number,
}: {
  org: string;
  repo: string;
  number: number;
}) {
  const action = addCommentAction.bind(null, org, repo, number);
  const [state, formAction] = useFormState(action, initialState);
  const ref = useRef<HTMLFormElement>(null);

  useEffect(() => {
    if (state.ok) ref.current?.reset();
  }, [state]);

  return (
    <form className="comment-form" action={formAction} ref={ref}>
      {state.error && <div className="form-error">{state.error}</div>}
      <textarea name="body" rows={3} placeholder="Leave a comment" />
      <div className="form-actions">
        <SubmitButton />
      </div>
    </form>
  );
}
