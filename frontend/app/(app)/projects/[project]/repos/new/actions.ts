"use server";

import { redirect } from "next/navigation";

import { createRepo } from "../../../../../lib/api";
import { getToken } from "../../../../../lib/session";

export type CreateState = { error?: string };

function slugify(input: string): string {
  return input
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 63);
}

// createRepoAction provisions a repository under an project, then redirects to its
// code page. The project slug is bound via the route param.
export async function createRepoAction(
  project: string,
  _prev: CreateState,
  formData: FormData,
): Promise<CreateState> {
  const token = getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const name = String(formData.get("name") ?? "").trim();
  const slug = slugify(String(formData.get("slug") ?? "") || name);
  const description = String(formData.get("description") ?? "").trim();
  const visibility = String(formData.get("visibility") ?? "private");

  if (!name) return { error: "Enter a repository name." };
  if (!slug) return { error: "Enter a valid slug (letters, digits, - _ .)." };

  const result = await createRepo(token, project, {
    slug,
    name,
    description,
    visibility,
  });
  if (!result.ok) return { error: result.error };

  redirect(`/projects/${project}/repos/${result.slug}`);
}
