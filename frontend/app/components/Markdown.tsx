import { renderMarkdown } from "../lib/api";

// Markdown renders user-written text (PR/issue descriptions, comments,
// reviews) as GitHub-flavoured markdown through the repo's markup endpoint
// (goldmark, raw HTML escaped server-side, so the output is safe to inject).
// When rendering is unavailable the text still reads well: proportional
// font, line breaks kept.
export async function Markdown({
  token,
  project,
  repo,
  text,
  className,
}: {
  token: string | null | undefined;
  project: string;
  repo: string;
  text: string;
  className?: string;
}) {
  const cls = className ? ` ${className}` : "";
  const html = text.trim()
    ? await renderMarkdown(token, project, repo, text)
    : null;
  if (html) {
    return (
      <div
        className={`markdown-body text-body${cls}`}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    );
  }
  return <div className={`text-body plain${cls}`}>{text}</div>;
}
