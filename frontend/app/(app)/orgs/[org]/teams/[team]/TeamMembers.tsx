"use client";

import { useFormState, useFormStatus } from "react-dom";

import type { TeamMember } from "../../../../../lib/api";
import {
  addTeamMemberAction,
  removeTeamMemberAction,
  type MemberFormState,
} from "./actions";

const initial: MemberFormState = {};

function AddButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Adding…" : "Add member"}
    </button>
  );
}

function RemoveButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn ghost" type="submit" disabled={pending}>
      {pending ? "Removing…" : "Remove"}
    </button>
  );
}

// RemoveMemberForm wraps a single member's remove action with its own form state
// so each row reports its own error without re-rendering the others.
function RemoveMemberForm({
  org,
  team,
  member,
}: {
  org: string;
  team: string;
  member: TeamMember;
}) {
  const action = removeTeamMemberAction.bind(null, org, team, member.id);
  const [state, formAction] = useFormState(action, initial);
  return (
    <form action={formAction}>
      <RemoveButton />
      {state.error && <span className="form-error">{state.error}</span>}
    </form>
  );
}

// TeamMembers renders a team's member roster with an add form and per-row remove
// controls. All mutations are server actions that revalidate the page.
export function TeamMembers({
  org,
  team,
  members,
}: {
  org: string;
  team: string;
  members: TeamMember[];
}) {
  const addAction = addTeamMemberAction.bind(null, org, team);
  const [addState, addFormAction] = useFormState(addAction, initial);

  return (
    <>
      <div className="panel form-narrow">
        <h2>Add a member</h2>
        <div className="readme-body">
          {addState.error && <div className="form-error">{addState.error}</div>}
          {addState.ok && <div className="form-success">Member added.</div>}
          <form action={addFormAction}>
            <label className="field">
              <span>Username</span>
              <input name="username" required placeholder="octocat" />
            </label>
            <label className="field">
              <span>Role</span>
              <select name="role" defaultValue="member">
                <option value="member">Member</option>
                <option value="maintainer">Maintainer</option>
              </select>
            </label>
            <div className="form-actions">
              <AddButton />
            </div>
          </form>
        </div>
      </div>

      <div className="panel">
        <h2>
          Members
          <span className="tag">{members.length}</span>
        </h2>
        {members.length === 0 ? (
          <div className="empty">No members yet. Add one above.</div>
        ) : (
          members.map((m) => (
            <div className="row-item" key={m.id}>
              <span className="tree-icon">◍</span>
              <span className="nm">{m.displayName || m.username}</span>
              <span className="sub">· {m.username}</span>
              <span className="tag">{m.role}</span>
              <span className="spacer" />
              <RemoveMemberForm org={org} team={team} member={m} />
            </div>
          ))
        )}
      </div>
    </>
  );
}
