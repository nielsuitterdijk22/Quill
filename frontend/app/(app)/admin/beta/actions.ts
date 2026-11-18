"use server";

import { revalidatePath } from "next/cache";

import { deleteResource, sendData } from "../../../lib/api/client";
import { getToken } from "../../../lib/session";

export type CreateBetaState = { link?: string; error?: string };

// createBetaInviteAction mints an invite (platform admins only — the backend
// enforces it). The link is shown once; only its hash is stored.
export async function createBetaInviteAction(
  _prev: CreateBetaState,
  formData: FormData,
): Promise<CreateBetaState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };
  const res = await sendData<{ link: string }>(token, "POST", "/api/v1/admin/beta-invites", {
    note: String(formData.get("note") ?? ""),
    maxUses: Number(formData.get("maxUses") ?? 1),
    days: Number(formData.get("days") ?? 14),
  });
  if (!res.ok) return { error: res.error };
  revalidatePath("/admin/beta");
  return { link: res.data.link };
}

export async function revokeBetaInviteAction(id: string): Promise<{ ok: true } | { ok: false; error: string }> {
  const token = await getToken();
  if (!token) return { ok: false, error: "Your session has expired. Sign in again." };
  const res = await deleteResource(token, `/api/v1/admin/beta-invites/${encodeURIComponent(id)}`);
  if (res.ok) revalidatePath("/admin/beta");
  return res;
}
