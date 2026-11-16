package ui

import (
	"fmt"
	"os"
	"strings"
)

// Pear uses raw ANSI escape codes only — zero third-party TUI deps (no lipgloss/bubbletea),
// consistent with the ecosystem's pure-stdlib design principle.

// ANSI SGR codes.
const (
	reset = "\033[0m"
	bold  = "\033[1m"
	dim   = "\033[2m"

	// Pear palette: fresh green body, warm brown stem, leaf accent.
	fgPearGreen  = "\033[38;5;112m" // 梨身主绿
	fgPearLight  = "\033[38;5;156m" // 高光浅绿
	fgLeaf       = "\033[38;5;71m"  // 叶片深绿
	fgStem       = "\033[38;5;130m" // 梨柄棕
	fgHi         = "\033[38;5;229m" // 高亮奶黄
	fgGood       = "\033[38;5;76m"  // 配额充足
	fgWarn       = "\033[38;5;214m" // 配额告警
	fgBad        = "\033[38;5;203m" // 配额耗尽
	fgMuted      = "\033[38;5;245m" // 次要信息灰
	fgCyan       = "\033[38;5;80m"  // 地址/链接
	trackBarChar = "░"
	fillBarChar  = "█"
)

// colorEnabled reports whether ANSI output should be emitted.
// Honors NO_COLOR (https://no-color.org) and non-TTY / dumb terminals.
func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	// On Windows 10+ the modern terminal/conhost supports VT; honor explicit opt-out only.
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// paint wraps s in the given SGR codes, stripping them entirely when color is disabled.
func paint(s string, codes ...string) string {
	if !colorEnabled() || len(codes) == 0 {
		return s
	}
	return strings.Join(codes, "") + s + reset
}

// Green returns bright pear-green text.
func Green(s string) string { return paint(s, fgPearGreen) }

// Muted returns dim grey secondary text.
func Muted(s string) string { return paint(s, fgMuted) }

// Bold returns bold text.
func Bold(s string) string { return paint(s, bold) }

// Accent returns cyan text for addresses / URLs.
func Accent(s string) string { return paint(s, fgCyan) }

// quotaColor picks a color code based on remaining percentage.
func quotaColor(remainPct float64) string {
	switch {
	case remainPct >= 40:
		return fgGood
	case remainPct >= 10:
		return fgWarn
	default:
		return fgBad
	}
}

// Bar renders a colored horizontal usage bar of the given width.
// remainPct is 0..100 (percentage of quota still available).
func Bar(remainPct float64, width int) string {
	if width < 4 {
		width = 4
	}
	if remainPct < 0 {
		remainPct = 0
	}
	if remainPct > 100 {
		remainPct = 100
	}
	filled := int(remainPct/100*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := strings.Repeat(fillBarChar, filled) + strings.Repeat(trackBarChar, empty)
	label := fmt.Sprintf(" %3.0f%%", remainPct)

	if !colorEnabled() {
		return "[" + bar + "]" + label
	}
	c := quotaColor(remainPct)
	return "[" + c + strings.Repeat(fillBarChar, filled) + reset +
		fgMuted + strings.Repeat(trackBarChar, empty) + reset + "]" +
		c + label + reset
}
