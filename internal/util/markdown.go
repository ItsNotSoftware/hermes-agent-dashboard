package util

import (
	"regexp"
	"strings"
)

var (
	mdComment    = regexp.MustCompile(`<!--.*?-->`)
	mdInlineCode = regexp.MustCompile("`([^`]*)`")
	mdImage      = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	mdLink       = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	mdWikiLink   = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	mdURL        = regexp.MustCompile(`https?://\S+`)
	mdSyntax     = regexp.MustCompile("[#*_~>|]")
	mdSpaces     = regexp.MustCompile(`\s+`)
)

// CompactTitle strips most markdown noise and truncates with an ellipsis,
// mirroring _compact_title in server.py.
func CompactTitle(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 84
	}
	t := s
	t = mdComment.ReplaceAllString(t, "")
	t = mdInlineCode.ReplaceAllString(t, "$1")
	t = mdImage.ReplaceAllString(t, "")
	t = mdLink.ReplaceAllString(t, "$1")
	t = mdWikiLink.ReplaceAllStringFunc(t, func(m string) string {
		sub := mdWikiLink.FindStringSubmatch(m)
		if len(sub) == 3 && sub[2] != "" {
			return sub[2]
		}
		if len(sub) >= 2 {
			return sub[1]
		}
		return m
	})
	t = mdURL.ReplaceAllString(t, "")
	t = mdSyntax.ReplaceAllString(t, " ")
	t = mdSpaces.ReplaceAllString(t, " ")
	t = strings.Trim(t, " -:\t")
	runes := []rune(t)
	if len(runes) > maxLen {
		t = strings.TrimRight(string(runes[:maxLen-1]), " ") + "…"
	}
	return t
}
