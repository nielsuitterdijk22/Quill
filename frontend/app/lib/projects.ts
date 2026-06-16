// Server-only current-project helpers. The selected project is stored in a
// readable (non-httpOnly) cookie so both server components and the client
// switcher agree on which project scopes the cross-cutting views (Repositories,
// Pull requests, Pipelines).
import { cookies } from "next/headers";

import { getMyProjects, type MyProject } from "./api";
import { getToken } from "./session";

export const CURRENT_PROJECT_COOKIE = "quill_current_project";

// resolveCurrentProject returns the projects the user belongs to together with
// the currently selected one. The selection comes from the cookie when it still
// names a project the user can access, otherwise the first project. Returns null
// when the user has no projects yet.
export async function resolveCurrentProject(
  token: string,
): Promise<{ current: MyProject; projects: MyProject[] } | null> {
  const projects = await getMyProjects(token);
  if (projects.length === 0) return null;
  const cookieSlug = cookies().get(CURRENT_PROJECT_COOKIE)?.value;
  const current =
    (cookieSlug && projects.find((p) => p.slug === cookieSlug)) || projects[0];
  return { current, projects };
}

// getCurrentProject returns just the current project slug, or null when the
// user is unauthenticated or has no projects.
export async function getCurrentProject(): Promise<string | null> {
  const token = getToken();
  if (!token) return null;
  const resolved = await resolveCurrentProject(token);
  return resolved?.current.slug ?? null;
}
