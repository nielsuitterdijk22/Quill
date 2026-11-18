"use server";

import { revalidatePath } from "next/cache";

import { sendData } from "../../../lib/api/client";
import { getToken } from "../../../lib/session";

// setPlanAction changes a tenant's plan (platform admins only — the backend
// enforces it). "internal" waives billing entirely: no caps, no
// checkout prompts; used for our own and test organizations.
export async function setPlanAction(slug: string, plan: string): Promise<{ ok: true } | { ok: false; error: string }> {
  const token = await getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };
  const res = await sendData<{ plan: string }>(token, "PUT", `/api/v1/orgs/${encodeURIComponent(slug)}/billing/plan`, { plan });
  if (!res.ok) return { ok: false, error: res.error };
  revalidatePath("/admin/analytics");
  return { ok: true };
}
