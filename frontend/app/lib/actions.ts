"use server";

import { revalidatePath } from "next/cache";
import { cookies } from "next/headers";

import { CURRENT_PROJECT_COOKIE } from "./projects";

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
