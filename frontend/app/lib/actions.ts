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

// startUpgradeAction starts a Creem checkout server-side (Team for orgs, Pro
// for personal accounts): calls the Go API with the session's bearer so it
// works independent of the /api/backend browser proxy; returns the checkout
// URL for the client to redirect to, or an error message that is shown
// honestly next to the button.
export async function startUpgradeAction(
  _prev: { url?: string; error?: string },
  formData: FormData,
): Promise<{ url?: string; error?: string }> {
  const endpoint = String(formData.get("endpoint") ?? "");
  if (!endpoint.startsWith("/")) return { error: "Invalid request." };
  const { getToken } = await import("./session");
  const token = await getToken();
  if (!token) return { error: "Your session has expired — sign in again." };
  const { API_BASE } = await import("./api/client");
  try {
    const res = await fetch(`${API_BASE}/api/v1${endpoint}`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    const data = (await res.json().catch(() => null)) as {
      url?: string;
      message?: string;
    } | null;
    if (res.ok && data?.url) return { url: data.url };
    if (res.status === 501) {
      return {
        error:
          "Payments aren't activated on this server yet — contact us to upgrade early.",
      };
    }
    return { error: data?.message ?? `Could not start checkout (${res.status}).` };
  } catch {
    return { error: "Could not reach the backend." };
  }
}
