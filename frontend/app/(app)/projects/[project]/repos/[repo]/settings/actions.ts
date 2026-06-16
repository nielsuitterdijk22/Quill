"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import {
  deleteBranchPolicy,
  deleteRepo,
  setBranchPolicy,
  updateRepo,
  type BranchPolicyInput,
  type UpdateRepoInput,
} from "../../../../../../lib/api";
import { getToken } from "../../../../../../lib/session";

export type PolicyFormState = { error?: string; ok?: boolean };

// setBranchPolicyAction creates or updates a branch policy, then refreshes the
// settings page. The project/repo slugs are bound from the route params.
export async function setBranchPolicyAction(
  project: string,
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
  };

  const res = await setBranchPolicy(token, project, repo, input);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}/settings`);
  return { ok: true };
}

// deleteBranchPolicyAction removes the policy for a pattern, then refreshes.
export async function deleteBranchPolicyAction(
  project: string,
  repo: string,
  _prev: PolicyFormState,
  formData: FormData,
): Promise<PolicyFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const pattern = String(formData.get("pattern") ?? "").trim();
  if (!pattern) return { error: "Missing policy pattern." };

  const res = await deleteBranchPolicy(token, project, repo, pattern);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}/settings`);
  return { ok: true };
}

export type RepoSettingsFormState = { error?: string; ok?: boolean };

const VISIBILITIES = new Set(["public", "internal", "private"]);

// updateRepoSettingsAction saves a repository's general settings (display name,
// description, and default branch), then refreshes its pages. Visibility lives in
// the danger zone (see changeVisibilityAction).
export async function updateRepoSettingsAction(
  project: string,
  repo: string,
  _prev: RepoSettingsFormState,
  formData: FormData,
): Promise<RepoSettingsFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const name = String(formData.get("name") ?? "").trim();
  if (!name) return { error: "Enter a display name." };

  const input: UpdateRepoInput = {
    name,
    description: String(formData.get("description") ?? "").trim(),
  };

  const defaultBranch = String(formData.get("defaultBranch") ?? "").trim();
  if (defaultBranch) input.defaultBranch = defaultBranch;

  const res = await updateRepo(token, project, repo, input);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}`, "layout");
  return { ok: true };
}

// changeVisibilityAction updates only a repository's visibility. It lives in the
// danger zone because flipping a private repository to public exposes its code.
export async function changeVisibilityAction(
  project: string,
  repo: string,
  _prev: RepoSettingsFormState,
  formData: FormData,
): Promise<RepoSettingsFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const visibility = String(formData.get("visibility") ?? "").trim();
  if (!VISIBILITIES.has(visibility))
    return { error: "Choose a valid visibility." };

  const res = await updateRepo(token, project, repo, { visibility });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}`, "layout");
  return { ok: true };
}

// renameRepoAction changes a repository's slug (and its Forgejo git repo), then
// redirects to the settings page at the new location.
export async function renameRepoAction(
  project: string,
  repo: string,
  _prev: RepoSettingsFormState,
  formData: FormData,
): Promise<RepoSettingsFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const slug = String(formData.get("slug") ?? "")
    .trim()
    .toLowerCase();
  if (!slug) return { error: "Enter a new repository name." };
  if (slug === repo) return { error: "That is already the repository name." };

  const res = await updateRepo(token, project, repo, { slug });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}`, "layout");
  redirect(`/projects/${project}/${res.data.slug}/settings`);
}

// setRepoArchivedAction toggles a repository's archived flag. The desired state
// is bound from the route so the same action backs both archive and unarchive.
export async function setRepoArchivedAction(
  project: string,
  repo: string,
  archived: boolean,
  _prev: RepoSettingsFormState,
  _formData: FormData,
): Promise<RepoSettingsFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const res = await updateRepo(token, project, repo, { archived });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/repos/${repo}`, "layout");
  return { ok: true };
}

// deleteRepoAction permanently removes a repository after the user retypes its
// name to confirm, then redirects to the project overview.
export async function deleteRepoAction(
  project: string,
  repo: string,
  _prev: RepoSettingsFormState,
  formData: FormData,
): Promise<RepoSettingsFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const confirm = String(formData.get("confirm") ?? "").trim();
  if (confirm !== repo)
    return { error: "Type the repository name to confirm." };

  const res = await deleteRepo(token, project, repo);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}`, "layout");
  redirect(`/projects/${project}`);
}
