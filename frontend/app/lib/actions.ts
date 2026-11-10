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

// toggleStarAction stars or unstars a repository server-side. Runs on the
// server with the session's bearer so it does not depend on the /api/backend
// browser proxy; revalidates the repo pages the count shows on.
export async function toggleStarAction(formData: FormData): Promise<void> {
  "use server";
  const { revalidatePath } = await import("next/cache");
  const project = String(formData.get("project") ?? "");
  const repo = String(formData.get("repo") ?? "");
  const starred = String(formData.get("starred") ?? "") === "1";
  if (!project || !repo) return;
  const { getToken } = await import("./session");
  const token = await getToken();
  if (!token) return;
  const { starRepo, unstarRepo } = await import("./api");
  if (starred) {
    await unstarRepo(token, project, repo);
  } else {
    await starRepo(token, project, repo);
  }
  revalidatePath(`/${project}/${repo}`);
  revalidatePath(`/projects/${project}/repos/${repo}`);
}
