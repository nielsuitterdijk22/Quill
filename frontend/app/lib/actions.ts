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

// startUpgradeAction starts a Creem checkout server-side (Team for orgs, Pro
// for personal accounts): calls the Go API with the session's bearer so it
// works independent of the /api/backend browser proxy; returns the checkout
// URL for the client to redirect to, or an error message that is shown
// honestly next to the button.
export async function startUpgradeAction(
  _prev: { url?: string; error?: string },
  formData: FormData,
): Promise<{ url?: string; error?: string }> {
  // Callers may pass either an API-relative path (/me/billing/checkout) or the
  // legacy browser-proxy form (/api/backend/me/billing/checkout); normalize so
  // the fetch below never double-prefixes with API_BASE + /api/v1.
  const endpoint = String(formData.get("endpoint") ?? "").replace(
    /^\/api\/backend(?=\/)/,
    "",
  );
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
