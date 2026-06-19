"use server";

import { revalidatePath } from "next/cache";

import { changePassword, updateEmail, updateProfile } from "../../lib/api";
import { getToken } from "../../lib/session";

export type ProfileFormState = { error?: string; ok?: boolean };
export type EmailFormState = { error?: string; ok?: boolean };
export type PasswordFormState = { error?: string; ok?: boolean };

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

// updateEmailAction changes the signed-in user's email address.
export async function updateEmailAction(
  _prev: EmailFormState,
  formData: FormData,
): Promise<EmailFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const email = String(formData.get("email") ?? "").trim();
  if (!email) return { error: "Email address is required." };

  const res = await updateEmail(token, email);
  if (!res.ok) return { error: res.error };

  revalidatePath("/", "layout");
  return { ok: true };
}

// changePasswordAction verifies the current password and sets a new one.
export async function changePasswordAction(
  _prev: PasswordFormState,
  formData: FormData,
): Promise<PasswordFormState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const currentPassword = String(formData.get("currentPassword") ?? "");
  const newPassword = String(formData.get("newPassword") ?? "");
  const confirmPassword = String(formData.get("confirmPassword") ?? "");

  if (!currentPassword || !newPassword || !confirmPassword) {
    return { error: "All password fields are required." };
  }
  if (newPassword !== confirmPassword) {
    return { error: "New passwords do not match." };
  }

  const res = await changePassword(token, currentPassword, newPassword);
  if (!res.ok) return { error: res.error };
  return { ok: true };
}
