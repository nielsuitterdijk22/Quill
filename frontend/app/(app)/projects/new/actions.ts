"use server";

import { redirect } from "next/navigation";

import { createProject } from "../../../lib/api";
import { slugify } from "../../../lib/slug";
import { getToken } from "../../../lib/session";

export type CreateState = { error?: string };

// createProjectAction provisions an project for the signed-in user, then
// redirects to its repository list. Validation errors are returned for display.
export async function createProjectAction(
  _prev: CreateState,
  formData: FormData,
): Promise<CreateState> {
  const token = await getToken();
  if (!token) return { error: "Your session has expired. Sign in again." };

  const name = String(formData.get("name") ?? "").trim();
  const slug = slugify(name);
  const description = String(formData.get("description") ?? "").trim();

  if (!name) return { error: "Enter an project name." };
  if (!slug) return { error: "Use a name with at least one letter or digit." };

  const result = await createProject(token, { slug, name, description });
  if (!result.ok) return { error: result.error };

  redirect(`/projects/${result.slug}`);
}
