package notify

import (
	"bytes"
	"fmt"
	"html/template"
)

var emailWrap = template.Must(template.New("wrap").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  body { font-family: -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; background:#f5f5f5; margin:0; padding:24px; }
  .card { background:#fff; border-radius:8px; max-width:560px; margin:0 auto; padding:32px; box-shadow:0 1px 4px rgba(0,0,0,.08); }
  h2 { font-size:18px; margin:0 0 16px; color:#111; }
  p { margin:0 0 12px; color:#444; font-size:14px; line-height:1.6; }
  .meta { color:#888; font-size:13px; }
  .btn { display:inline-block; margin-top:20px; padding:10px 20px; background:#0070f3; color:#fff; text-decoration:none; border-radius:6px; font-size:14px; }
  .excerpt { background:#f8f8f8; border-left:3px solid #ddd; padding:10px 14px; border-radius:4px; font-size:13px; color:#555; margin:12px 0; white-space:pre-wrap; word-break:break-word; }
  .footer { margin-top:24px; font-size:12px; color:#aaa; text-align:center; }
</style>
</head>
<body>
<div class="card">
  {{.Body}}
  <div class="footer">You received this because you are a member of a Quill organisation.<br>Quill — collaborative code hosting</div>
</div>
</body>
</html>`))

func render(body string) string {
	var buf bytes.Buffer
	_ = emailWrap.Execute(&buf, map[string]template.HTML{"Body": template.HTML(body)})
	return buf.String()
}

func renderPRReview(reviewer, action, prTitle string, prNumber int, repoSlug, link string) string {
	body := fmt.Sprintf(
		`<h2>Pull request review</h2>
<p><strong>%s</strong> %s your pull request in <span class="meta">%s</span>.</p>
<p class="meta">PR #%d — %s</p>`,
		template.HTMLEscapeString(reviewer),
		template.HTMLEscapeString(action),
		template.HTMLEscapeString(repoSlug),
		prNumber,
		template.HTMLEscapeString(prTitle),
	)
	if link != "" {
		body += fmt.Sprintf(`<a class="btn" href="%s">View pull request</a>`, template.HTMLEscapeString(link))
	}
	return render(body)
}

func renderPRComment(commenter, prTitle string, prNumber int, repoSlug, link string) string {
	body := fmt.Sprintf(
		`<h2>New comment on your pull request</h2>
<p><strong>%s</strong> commented on your pull request in <span class="meta">%s</span>.</p>
<p class="meta">PR #%d — %s</p>`,
		template.HTMLEscapeString(commenter),
		template.HTMLEscapeString(repoSlug),
		prNumber,
		template.HTMLEscapeString(prTitle),
	)
	if link != "" {
		body += fmt.Sprintf(`<a class="btn" href="%s">View pull request</a>`, template.HTMLEscapeString(link))
	}
	return render(body)
}

func renderCIFailure(workflowPath, ref, repoSlug string, runNumber int64, link string) string {
	body := fmt.Sprintf(
		`<h2>Pipeline run failed</h2>
<p>A pipeline run failed in <strong>%s</strong>.</p>
<p class="meta">Workflow: %s<br>Branch / ref: %s<br>Run #%d</p>`,
		template.HTMLEscapeString(repoSlug),
		template.HTMLEscapeString(workflowPath),
		template.HTMLEscapeString(ref),
		runNumber,
	)
	if link != "" {
		body += fmt.Sprintf(`<a class="btn" href="%s">View run logs</a>`, template.HTMLEscapeString(link))
	}
	return render(body)
}

func renderMention(mentioner, contextKind, contextTitle, excerpt, link string) string {
	body := fmt.Sprintf(
		`<h2>You were mentioned</h2>
<p><strong>%s</strong> mentioned you in a %s: <em>%s</em></p>`,
		template.HTMLEscapeString(mentioner),
		template.HTMLEscapeString(contextKind),
		template.HTMLEscapeString(contextTitle),
	)
	if excerpt != "" {
		body += fmt.Sprintf(`<div class="excerpt">%s</div>`, template.HTMLEscapeString(excerpt))
	}
	if link != "" {
		body += fmt.Sprintf(`<a class="btn" href="%s">View %s</a>`, template.HTMLEscapeString(link), template.HTMLEscapeString(contextKind))
	}
	return render(body)
}
