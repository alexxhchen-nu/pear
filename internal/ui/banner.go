package ui

import (
	"fmt"
	"strings"
)

// pearArt is the green ASCII-art pear logo. Lines are colored per-segment:
// leaf (deep green) + stem (brown) on top, pear body (bright green) below.
// Kept deliberately compact so it fits an 80-col terminal with the title block.
var pearArt = []struct {
	text  string
	codes []string
}{
	{`        .-.`, []string{fgLeaf}},
	{`       (   )  __`, []string{fgLeaf}},
	{`        '-'  //`, []string{fgStem}},
	{`            //`, []string{fgStem}},
	{`          .==.`, []string{fgPearLight}},
	{`        .'    '.`, []string{fgPearLight}},
	{`       /        \`, []string{fgPearGreen}},
	{`      |          |`, []string{fgPearGreen}},
	{`      |          |`, []string{fgPearGreen}},
	{`       \        /`, []string{fgPearGreen}},
	{`        '.____.'`, []string{fgPearGreen}},
}

// titleLines sit to the right of the pear art.
var titleLines = []string{
	"",
	"",
	"",
	fgPearLight + bold + "  P E A R" + reset,
	fgMuted + "  ────────────────" + reset,
	fgPearGreen + "  极简 AI 路由网关" + reset,
	fgMuted + "  & OAuth 订阅池" + reset,
	"",
	fgCyan + "  zero-dep · pure-go" + reset,
	"",
	"",
}

// Banner returns the full colored pear logo + title block as a printable string.
func Banner() string {
	var b strings.Builder
	n := len(pearArt)
	if len(titleLines) > n {
		n = len(titleLines)
	}
	for i := 0; i < n; i++ {
		artLine := ""
		if i < len(pearArt) {
			a := pearArt[i]
			artLine = paint(a.text, a.codes...)
			// pad art column to a fixed width for alignment (strip-aware: use raw text len)
			if pad := 20 - len([]rune(a.text)); pad > 0 {
				artLine += strings.Repeat(" ", pad)
			}
		} else {
			artLine = strings.Repeat(" ", 20)
		}
		title := ""
		if i < len(titleLines) {
			title = titleLines[i]
			if !colorEnabled() {
				title = stripANSI(title)
			}
		}
		b.WriteString(artLine)
		b.WriteString(title)
		b.WriteString("\n")
	}
	return b.String()
}

// stripANSI removes SGR sequences for no-color fallback.
func stripANSI(s string) string {
	for {
		i := strings.Index(s, "\033[")
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], 'm')
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

// SectionHeader renders a labeled divider line, e.g.  "── OAuth 订阅池 ──────".
func SectionHeader(label string) string {
	title := paint(" "+label+" ", fgPearLight, bold)
	rule := paint(strings.Repeat("─", 48), fgMuted)
	if colorEnabled() {
		return "\n" + paint("──", fgMuted) + title + rule + "\n"
	}
	return "\n── " + label + " " + strings.Repeat("─", 44) + "\n"
}

// KV renders an aligned "key : value" line.
func KV(key, value string) string {
	return fmt.Sprintf("  %s %s %s",
		Muted(fmt.Sprintf("%-16s", key)),
		Muted(":"),
		value,
	)
}
