"use server";

import { revalidatePath } from "next/cache";

import {
  deleteBranchPolicy,
  deleteProjectPolicy,
  deleteTenantPolicy,
  setBranchPolicy,
  setProjectPolicy,
  setTenantPolicy,
  type BranchPolicyInput,
} from "../../lib/api";
import { getToken } from "../../lib/session";

// PolicyTarget identifies which scope a branch-policy form acts on. It is passed
// to the shared actions (bound from route params) so one component can manage
// repo, project, and tenant policies.
export type PolicyTarget =
  | { scope: "repo"; project: string; repo: string }
  | { scope: "project"; project: string }
  | { scope: "tenant"; tenant: string };

export type PolicyFormState = { error?: string; ok?: boolean };

// revalidateTarget refreshes the settings page that owns a target's policies.
function revalidateTarget(target: PolicyTarget) {
  switch (target.scope) {
    case "repo":
      revalidatePath(`/projects/${target.project}/repos/${target.repo}/settings`);
      break;
    case "project":
      revalidatePath(`/projects/${target.project}/settings`);
      break;
    case "tenant":
      revalidatePath(`/admin/policies`);
      break;
  }
}

// savePolicyAction creates or updates a branch policy at the bound scope.
export async function savePolicyAction(
  target: PolicyTarget,
  _prev: PolicyFormState,
  formData: FormData,
): Promise<PolicyFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const pattern = String(formData.get("pattern") ?? "").trim();
  if (!pattern) return { error: "Enter a branch name or glob pattern." };

  const approvalsRaw = String(formData.get("requiredApprovals") ?? "0").trim();
  const requiredApprovals = Number(approvalsRaw);
  if (!Number.isInteger(requiredApprovals) || requiredApprovals < 0) {
    return {
      error: "Required approvals must be zero or a positive whole number.",
    };
  }

  const input: BranchPolicyInput = {
    pattern,
    requiredApprovals,
    requirePullRequest: formData.get("requirePullRequest") === "on",
    blockForcePush: formData.get("blockForcePush") === "on",
    dismissStaleApprovals: formData.get("dismissStaleApprovals") === "on",
    requireUpToDate: formData.get("requireUpToDate") === "on",
    requireStatusChecks: formData.get("requireStatusChecks") === "on",
    // Locking is only offered at project and tenant scope.
    locked: target.scope !== "repo" && formData.get("locked") === "on",
  };

  const res =
    target.scope === "repo"
      ? await setBranchPolicy(token, target.project, target.repo, input)
      : target.scope === "project"
        ? await setProjectPolicy(token, target.project, input)
        : await setTenantPolicy(token, target.tenant, input);
  if (!res.ok) return { error: res.error };

  revalidateTarget(target);
  return { ok: true };
}

// deletePolicyAction removes a branch policy at the bound scope.
export async function deletePolicyAction(
  target: PolicyTarget,
  _prev: PolicyFormState,
  formData: FormData,
): Promise<PolicyFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const pattern = String(formData.get("pattern") ?? "").trim();
  if (!pattern) return { error: "Missing policy pattern." };

  const res =
    target.scope === "repo"
      ? await deleteBranchPolicy(token, target.project, target.repo, pattern)
      : target.scope === "project"
        ? await deleteProjectPolicy(token, target.project, pattern)
        : await deleteTenantPolicy(token, target.tenant, pattern);
  if (!res.ok) return { error: res.error };

  revalidateTarget(target);
  return { ok: true };
}
