"use server";

import { revalidatePath } from "next/cache";

import {
  createEnvironment,
  deleteEnvironment,
  updateEnvironment,
} from "../../lib/api";
import { getToken } from "../../lib/session";

// Server actions backing the EnvironmentManager. Environments are project-owned,
// so every action is bound to a project slug from the settings route. Writes
// require a project admin (enforced by the backend); the actions surface its
// errors as form state.

export type EnvironmentFormState = { error?: string; ok?: boolean };

function parseRank(raw: string): number | null {
  const n = Number(String(raw ?? "0").trim());
  if (!Number.isInteger(n) || n < 0) return null;
  return n;
}

// createEnvironmentAction defines a new environment under the bound project.
export async function createEnvironmentAction(
  project: string,
  _prev: EnvironmentFormState,
  formData: FormData,
): Promise<EnvironmentFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const slug = String(formData.get("slug") ?? "").trim();
  if (!slug) return { error: "Enter an environment slug, e.g. production." };

  const rank = parseRank(String(formData.get("rank") ?? "0"));
  if (rank === null) {
    return { error: "Rank must be zero or a positive whole number." };
  }

  const res = await createEnvironment(token, project, {
    slug,
    name: String(formData.get("name") ?? "").trim(),
    description: String(formData.get("description") ?? "").trim(),
    rank,
  });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/settings`);
  return { ok: true };
}

// updateEnvironmentAction edits an environment's display fields and rank. The
// slug is immutable and supplied as a hidden field to address the target.
export async function updateEnvironmentAction(
  project: string,
  _prev: EnvironmentFormState,
  formData: FormData,
): Promise<EnvironmentFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const slug = String(formData.get("slug") ?? "").trim();
  if (!slug) return { error: "Missing environment slug." };

  const rank = parseRank(String(formData.get("rank") ?? "0"));
  if (rank === null) {
    return { error: "Rank must be zero or a positive whole number." };
  }

  const res = await updateEnvironment(token, project, slug, {
    name: String(formData.get("name") ?? "").trim(),
    description: String(formData.get("description") ?? "").trim(),
    rank,
  });
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/settings`);
  return { ok: true };
}

// deleteEnvironmentAction removes an environment from the bound project.
export async function deleteEnvironmentAction(
  project: string,
  _prev: EnvironmentFormState,
  formData: FormData,
): Promise<EnvironmentFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const slug = String(formData.get("slug") ?? "").trim();
  if (!slug) return { error: "Missing environment slug." };

  const res = await deleteEnvironment(token, project, slug);
  if (!res.ok) return { error: res.error };

  revalidatePath(`/projects/${project}/settings`);
  return { ok: true };
}
