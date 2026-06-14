"use server";

import { revalidatePath } from "next/cache";

import {
  deleteBranchPolicy,
  setBranchPolicy,
  type BranchPolicyInput,
} from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";

export type PolicyFormState = { error?: string; ok?: boolean };

// setBranchPolicyAction creates or updates a branch policy, then refreshes the
// settings page. The org/repo slugs are bound from the route params.
export async function setBranchPolicyAction(
  org: string,
  repo: string,
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
    return { error: "Required approvals must be zero or a positive whole number." };
  }

  const input: BranchPolicyInput = {
    pattern,
    requiredApprovals,
    requirePullRequest: formData.get("requirePullRequest") === "on",
    blockForcePush: formData.get("blockForcePush") === "on",
    dismissStaleApprovals: formData.get("dismissStaleApprovals") === "on",
    requireUpToDate: formData.get("requireUpToDate") === "on",
  };

  const res = await setBranchPolicy(token, org, repo, input);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/orgs/${org}/${repo}/settings`);
  return { ok: true };
}

// deleteBranchPolicyAction removes the policy for a pattern, then refreshes.
export async function deleteBranchPolicyAction(
  org: string,
  repo: string,
  _prev: PolicyFormState,
  formData: FormData,
): Promise<PolicyFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const pattern = String(formData.get("pattern") ?? "").trim();
  if (!pattern) return { error: "Missing policy pattern." };

  const res = await deleteBranchPolicy(token, org, repo, pattern);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/orgs/${org}/${repo}/settings`);
  return { ok: true };
}
