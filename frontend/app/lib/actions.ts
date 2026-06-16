"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { cookies } from "next/headers";

import { clearSessionCookie } from "./session";
import { CURRENT_PROJECT_COOKIE } from "./projects";

// logoutAction clears the session cookie and returns to the login screen. Tokens
// are stateless, so there is nothing server-side to revoke.
export async function logoutAction(): Promise<void> {
  clearSessionCookie();
  redirect("/login");
}

// setCurrentProjectAction persists the selected project in a cookie so server
// components scope their listings to it, then revalidates the shell so the
// current page re-renders against the new project.
export async function setCurrentProjectAction(
  formData: FormData,
): Promise<void> {
  const slug = String(formData.get("project") ?? "");
  if (slug) {
    cookies().set(CURRENT_PROJECT_COOKIE, slug, {
      sameSite: "lax",
      secure: process.env.NODE_ENV === "production",
      path: "/",
      maxAge: 60 * 60 * 24 * 365,
    });
  }
  revalidatePath("/", "layout");
}
