// slugify derives a URL/git-safe slug from free text. It mirrors the backend's
// SlugFromName + validSlug (backend/internal/platform/platform.go): people never
// type slugs, they type names; the backend re-validates everything.
export function slugify(input: string): string {
  let s = input
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/\.{2,}/g, ".")
    .slice(0, 63)
    .replace(/^[-._]+|[-._]+$/g, "");
  for (const suffix of [".git", ".atom"]) {
    if (s.endsWith(suffix)) s = s.slice(0, -suffix.length).replace(/^[-._]+|[-._]+$/g, "");
  }
  return s;
}

// isValidHandle matches the backend's username/slug rule (lowercase letters,
// digits, '.', '_' and '-', starting alphanumeric, up to 63 characters).
export function isValidHandle(s: string): boolean {
  return /^[a-z0-9][a-z0-9._-]{0,62}$/.test(s) && !s.endsWith(".") && !s.includes("..") && !s.endsWith(".git") && !s.endsWith(".atom");
}
