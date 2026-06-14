"use server";

import { redirect } from "next/navigation";

import { clearSessionCookie } from "./session";

// logoutAction clears the session cookie and returns to the login screen. Tokens
// are stateless, so there is nothing server-side to revoke.
export async function logoutAction(): Promise<void> {
  clearSessionCookie();
  redirect("/login");
}
