"use server";

import { revalidatePath } from "next/cache";

import {
  deleteEnvironmentPolicy,
  deleteProjectEnvironmentPolicy,
  deleteTenantEnvironmentPolicy,
  setEnvironmentPolicy,
  setProjectEnvironmentPolicy,
  setTenantEnvironmentPolicy,
  type EnvironmentPolicyInput,
} from "../../lib/api";
import { getToken } from "../../lib/session";
import type { PolicyFormState, PolicyTarget } from "./actions";

// This mirrors ./actions.ts but for environment (deploy-gate) policies. It reuses
// the shared PolicyTarget/PolicyFormState types so the same scope-addressing
// model (repo / project / tenant) backs both policy kinds.

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

// parseSources splits the comma/newline-separated allowed-source-branches field
// into a trimmed, de-duplicated glob list. The server validates each glob.
function parseSources(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of raw.split(/[\n,]/)) {
    const s = part.trim();
    if (s && !seen.has(s)) {
      seen.add(s);
      out.push(s);
    }
  }
  return out;
}

// saveEnvironmentPolicyAction creates or updates an environment policy at the
// bound scope.
export async function saveEnvironmentPolicyAction(
  target: PolicyTarget,
  _prev: PolicyFormState,
  formData: FormData,
): Promise<PolicyFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const pattern = String(formData.get("pattern") ?? "").trim();
  if (!pattern) return { error: "Enter an environment name or glob pattern." };

  const approvalsRaw = String(formData.get("requiredApprovals") ?? "0").trim();
  const requiredApprovals = Number(approvalsRaw);
  if (!Number.isInteger(requiredApprovals) || requiredApprovals < 0) {
    return {
      error: "Required approvals must be zero or a positive whole number.",
    };
  }

  const waitRaw = String(formData.get("minWaitMinutes") ?? "0").trim();
  const minWaitMinutes = Number(waitRaw);
  if (!Number.isInteger(minWaitMinutes) || minWaitMinutes < 0) {
    return {
      error: "Wait window must be zero or a positive whole number of minutes.",
    };
  }

  const input: EnvironmentPolicyInput = {
    pattern,
    requiredApprovals,
    allowedSourceBranches: parseSources(
      String(formData.get("allowedSourceBranches") ?? ""),
    ),
    requirePreviousEnvironment: String(
      formData.get("requirePreviousEnvironment") ?? "",
    ).trim(),
    requireSuccessfulRun: formData.get("requireSuccessfulRun") === "on",
    minWaitMinutes,
    // Locking is only offered at project and tenant scope.
    locked: target.scope !== "repo" && formData.get("locked") === "on",
  };

  const res =
    target.scope === "repo"
      ? await setEnvironmentPolicy(token, target.project, target.repo, input)
      : target.scope === "project"
        ? await setProjectEnvironmentPolicy(token, target.project, input)
        : await setTenantEnvironmentPolicy(token, target.tenant, input);
  if (!res.ok) return { error: res.error };

  revalidateTarget(target);
  return { ok: true };
}

// deleteEnvironmentPolicyAction removes an environment policy at the bound scope.
export async function deleteEnvironmentPolicyAction(
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
      ? await deleteEnvironmentPolicy(token, target.project, target.repo, pattern)
      : target.scope === "project"
        ? await deleteProjectEnvironmentPolicy(token, target.project, pattern)
        : await deleteTenantEnvironmentPolicy(token, target.tenant, pattern);
  if (!res.ok) return { error: res.error };

  revalidateTarget(target);
  return { ok: true };
}
