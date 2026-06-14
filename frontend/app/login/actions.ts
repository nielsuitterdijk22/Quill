"use server";

import { redirect } from "next/navigation";

import { login } from "../lib/api";
import { setSessionCookie } from "../lib/session";

export type AuthFormState = { error?: string };

// loginAction authenticates the submitted credentials, stores the token in an
// httpOnly cookie, and redirects into the app. Errors are returned for display.
export async function loginAction(
  _prev: AuthFormState,
  formData: FormData,
): Promise<AuthFormState> {
  const username = String(formData.get("username") ?? "").trim();
  const password = String(formData.get("password") ?? "");
  if (!username || !password) {
    return { error: "Enter your username and password." };
  }

  const result = await login(username, password);
  if (!result.ok) {
    return { error: result.error };
  }

  setSessionCookie(result.token);
  redirect("/");
}
