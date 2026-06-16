"use server";

import { revalidatePath } from "next/cache";

import { updateProfile } from "../../lib/api";
import { getToken } from "../../lib/session";

export type ProfileFormState = { error?: string; ok?: boolean };

// updateProfileAction saves the signed-in user's display name, then refreshes the
// app shell so the sidebar reflects the new name.
export async function updateProfileAction(
  _prev: ProfileFormState,
  formData: FormData,
): Promise<ProfileFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const displayName = String(formData.get("displayName") ?? "").trim();

  const res = await updateProfile(token, { displayName });
  if (!res.ok) return { error: res.error };

  revalidatePath("/", "layout");
  return { ok: true };
}
