"use server";

import { redirect } from "next/navigation";

import { register } from "../lib/api";
import { setSessionCookie } from "../lib/session";

export type AuthFormState = { error?: string };

// registerAction creates an account, stores the issued token, and redirects into
// the app. The backend makes the first account an admin.
export async function registerAction(
  _prev: AuthFormState,
  formData: FormData,
): Promise<AuthFormState> {
  const username = String(formData.get("username") ?? "").trim();
  const email = String(formData.get("email") ?? "").trim();
  const displayName = String(formData.get("displayName") ?? "").trim();
  const password = String(formData.get("password") ?? "");

  if (!username || !email || !password) {
    return { error: "Username, email, and password are required." };
  }

  const result = await register({ username, email, displayName, password });
  if (!result.ok) {
    return { error: result.error };
  }

  setSessionCookie(result.token);
  redirect("/");
}
