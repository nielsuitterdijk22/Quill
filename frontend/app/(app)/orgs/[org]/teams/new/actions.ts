"use server";

import { redirect } from "next/navigation";

import { createTeam } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";

export type CreateTeamState = { error?: string };

// slugify derives a URL-safe slug from free text, matching the backend's slug
// rules (lowercase alphanumerics plus -, _, .).
function slugify(input: string): string {
  return input
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 63);
}

// createTeamAction provisions a team within an org, then redirects to its detail
// page. Validation errors are returned for display. The org slug is bound from
// the route params.
export async function createTeamAction(
  org: string,
  _prev: CreateTeamState,
  formData: FormData,
): Promise<CreateTeamState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const name = String(formData.get("name") ?? "").trim();
  const slug = slugify(String(formData.get("slug") ?? "") || name);
  const description = String(formData.get("description") ?? "").trim();

  if (!name) return { error: "Enter a team name." };
  if (!slug) return { error: "Enter a valid slug (letters, digits, - _ .)." };

  const result = await createTeam(token, org, { slug, name, description });
  if (!result.ok) return { error: result.error };

  redirect(`/orgs/${org}/teams/${result.slug}`);
}
