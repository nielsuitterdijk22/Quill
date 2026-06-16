"use server";

import { redirect } from "next/navigation";

import { createProject } from "../../../lib/api";
import { getToken } from "../../../lib/session";

export type CreateState = { error?: string };

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

// createProjectAction provisions an project for the signed-in user, then
// redirects to its repository list. Validation errors are returned for display.
export async function createProjectAction(
  _prev: CreateState,
  formData: FormData,
): Promise<CreateState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const name = String(formData.get("name") ?? "").trim();
  const slug = slugify(String(formData.get("slug") ?? "") || name);
  const description = String(formData.get("description") ?? "").trim();

  if (!name) return { error: "Enter an project name." };
  if (!slug) return { error: "Enter a valid slug (letters, digits, - _ .)." };

  const result = await createProject(token, { slug, name, description });
  if (!result.ok) return { error: result.error };

  redirect(`/projects/${result.slug}`);
}
