"use server";

import { revalidatePath } from "next/cache";

import { setRepoEgress, setTenantEgress, type EgressMode } from "../../lib/api";
import { getToken } from "../../lib/session";

// Server actions backing the egress settings. The backend authorizes
// (org admins / the account owner for the organization policy, project
// admins for a repository's extra hosts) and validates each host.

export type EgressTarget =
  | { scope: "tenant"; tenant: string | null; path: string }
  | { scope: "repo"; project: string; repo: string; path: string };

export type EgressFormState = { error?: string; ok?: string };

function hostsFrom(formData: FormData): string[] {
  return String(formData.get("hosts") ?? "")
    .split(/[\s,]+/)
    .map((h) => h.trim())
    .filter(Boolean);
}

export async function saveEgressAction(
  target: EgressTarget,
  _prev: EgressFormState,
  formData: FormData,
): Promise<EgressFormState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  if (target.scope === "repo") {
    const res = await setRepoEgress(token, target.project, target.repo, hostsFrom(formData));
    if (!res.ok) return { error: res.error };
    revalidatePath(target.path);
    return { ok: "Repository hosts saved. They apply to the next pipeline run." };
  }

  const mode: EgressMode = formData.get("mode") === "allow_all" ? "allow_all" : "allowlist";
  const reset = formData.get("intent") === "reset";
  const res = await setTenantEgress(token, target.tenant, mode, reset ? null : hostsFrom(formData));
  if (!res.ok) return { error: res.error };
  revalidatePath(target.path);
  return { ok: reset ? "Allowlist reset to the defaults." : "Egress policy saved. It applies to the next pipeline run." };
}
