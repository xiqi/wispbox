package security

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Email HTML policies. Two variants: remote content blocked (default) and
// remote content allowed (user explicitly clicked "load remote images").
// Neither ever allows script execution, event handlers, frames, or forms.
var (
	blockedPolicy = buildEmailPolicy(false)
	remotePolicy  = buildEmailPolicy(true)
	remoteImgRe   = regexp.MustCompile(`(?i)<img[^>]+src\s*=\s*["']?https?://`)
	cssURLRe      = regexp.MustCompile(`(?i)url\s*\(`)
)

func buildEmailPolicy(allowRemote bool) *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	p.AllowElements(
		"a", "abbr", "address", "b", "blockquote", "br", "caption", "center",
		"cite", "code", "col", "colgroup", "dd", "del", "details", "div", "dl",
		"dt", "em", "figcaption", "figure", "h1", "h2", "h3", "h4", "h5", "h6",
		"hr", "i", "img", "ins", "kbd", "li", "mark", "ol", "p", "pre", "q",
		"s", "small", "span", "strike", "strong", "sub", "summary", "sup",
		"table", "tbody", "td", "tfoot", "th", "thead", "time", "tr", "u", "ul",
	)

	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("https", "http", "mailto")
	// Embedded images (cid:, rewritten to data: or /api URLs by the message
	// renderer) must also pass the URL policy, not just the img src matcher.
	p.AllowURLSchemeWithCustomPolicy("data", func(u *url.URL) bool {
		return strings.HasPrefix(u.Opaque, "image/")
	})
	p.AllowRelativeURLs(true)
	p.RequireNoFollowOnLinks(true)
	p.AllowAttrs("target").OnElements("a")

	p.AllowAttrs("alt", "width", "height").OnElements("img")
	if allowRemote {
		p.AllowAttrs("src").OnElements("img")
	} else {
		// Only embedded (cid:, already rewritten to data: or /api URLs by the
		// message renderer) images survive; remote fetches are stripped.
		p.AllowAttrs("src").Matching(regexp.MustCompile(`^(data:image/|/api/mail/attachments/)`)).OnElements("img")
	}

	p.AllowAttrs("colspan", "rowspan").OnElements("td", "th")
	p.AllowAttrs("style").Globally()
	p.AllowStyles(
		"background", "background-color", "border", "border-radius", "color",
		"display", "font", "font-family", "font-size", "font-style",
		"font-weight", "height", "letter-spacing", "line-height", "margin",
		"max-width", "min-width", "padding", "text-align", "text-decoration",
		"vertical-align", "width",
	).Globally()

	return p
}

// SanitizeOutgoingHTML cleans HTML produced by the webmail rich-text editor
// before it leaves the server. The editor is first-party, but user HTML is
// never sent unsanitized: scripts, event handlers, iframes, and forms are
// always stripped, and only the formatting tags/styles the editor emits
// survive. Reuses the same allowlist as inbound rendering.
func SanitizeOutgoingHTML(html string) string {
	return remotePolicy.Sanitize(html)
}

// SanitizeEmailHTML strips scripts, event handlers, frames, and forms from
// email HTML. With allowRemote=false (the default), http(s) image sources are
// removed as a tracking/privacy measure. The second return value reports
// whether remote content was present so the UI can offer to load it.
func SanitizeEmailHTML(html string, allowRemote bool) (clean string, hadRemote bool) {
	hadRemote = remoteImgRe.MatchString(html)
	pre := html
	if !allowRemote {
		// Belt and braces: strip url(...) in inline styles so background
		// images can't be used as tracking pixels either.
		pre = cssURLRe.ReplaceAllString(pre, "no-url(")
	}
	if allowRemote {
		return remotePolicy.Sanitize(pre), hadRemote
	}
	return blockedPolicy.Sanitize(pre), hadRemote
}
