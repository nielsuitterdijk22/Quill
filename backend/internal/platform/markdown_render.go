package platform

import (
	"bytes"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Markdown rendering for engine-backed repos (serverless epic, PR B4).
// Forgejo repos keep the client's RenderMarkup until Phase D; engine repos
// render GFM locally with goldmark — the same MIT engine gitea/Forgejo build
// their markdown pipeline on, so output stays familiar.
//
// Safety: no WithUnsafe option → raw HTML in the source is escaped, so the
// output is sanitized by construction (the same default gitea relies on for
// untrusted render contexts). Repo-relative link resolution (a Forgejo
// server-side nicety) is frontend follow-up polish; links stay as authored.

var (
	gfmOnce sync.Once
	gfm     goldmark.Markdown
)

func gfmRenderer() goldmark.Markdown {
	gfmOnce.Do(func() {
		gfm = goldmark.New(
			goldmark.WithExtensions(extension.GFM, extension.Linkify),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
			// html.HardWraps + no WithUnsafe: raw HTML escaped.
			goldmark.WithRendererOptions(html.WithHardWraps()),
		)
	})
	return gfm
}

// renderGFM renders CommonMark+GFM (tables, strikethrough, task lists) to
// HTML. Render failures cannot occur for valid inputs (goldmark writes to an
// in-memory buffer); on a parse anomaly the raw text is returned as-is.
func renderGFM(text string) string {
	var buf bytes.Buffer
	if err := gfmRenderer().Convert([]byte(text), &buf); err != nil {
		return text
	}
	return buf.String()
}
