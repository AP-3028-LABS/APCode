package tui

import (
	"path/filepath"
	"strings"
)

// Input box geometry. The interactive REPL prints the top border and the
// prompt prefix, lets the terminal echo the user's typing inline (so all
// normal editing keys keep working), and closes the border after Enter.

// BoxWidth returns the input box width for a given terminal width,
// clamped so the box always fits.
func BoxWidth(termWidth int) int {
	if termWidth <= 0 {
		termWidth = defaultTermWidth
	}
	w := termWidth - 4
	return ClampWidth(w, 24, 76)
}

// InputBoxTop renders the top border of the input box.
func InputBoxTop(width int) string {
	if width < 6 {
		width = 6
	}
	return Border("╭" + strings.Repeat("─", width-2) + "╮")
}

// InputBoxPrefix renders the left border and prompt mark. The cursor sits
// immediately after it, ready for typed input.
func InputBoxPrefix() string {
	return Border("│") + "  " + Accent("›") + " "
}

// InputBoxBottom renders the bottom border of the input box.
func InputBoxBottom(width int) string {
	if width < 6 {
		width = 6
	}
	return Border("╰" + strings.Repeat("─", width-2) + "╯")
}

// InputBox renders a closed preview of the input box with the given text
// inside. Used by tests, help screens, and non-interactive rendering.
// Kept for backward compat; the welcome screen now uses InputBoxTwoRow.
func InputBox(width int, text string) string {
	var b strings.Builder
	b.WriteString(InputBoxTop(width))
	b.WriteByte('\n')
	for _, line := range strings.Split(text, "\n") {
		line = truncateVisible(line, width-6)
		pad := width - 6 - visibleWidth(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(Border("│") + "  " + Accent("› ") + line + strings.Repeat(" ", pad) + Border("│"))
		b.WriteByte('\n')
	}
	b.WriteString(InputBoxBottom(width))
	return b.String()
}

// --- OpenCode-inspired two-row input box (spec) ---

const (
	placeholderAsk     = "Ask anything..."
	placeholderExample = `"Fix broken tests"`
)

// InputPlaceholder returns the faded placeholder text for row 1.
// Kept as a helper so welcome and tests share the same literal.
func InputPlaceholder() string { return placeholderAsk + " " + placeholderExample }

// AttachmentChip returns the display string for an attached image, e.g. "[🖼 photo.png]".
// It is styled muted so it appears as a subtle chip in the input status line.
func AttachmentChip(imagePath string) string {
	if strings.TrimSpace(imagePath) == "" {
		return ""
	}
	base := filepath.Base(imagePath)
	if base == "." || base == "" {
		base = imagePath
	}
	return Muted("[🖼 " + base + "]")
}

// AttachmentChips renders multiple attachment chips joined by spaces, e.g.
// "[🖼 a.png] [🖼 b.png]". Empty/nil inputs yield an empty string.
func AttachmentChips(imagePaths []string) string {
	var chips []string
	for _, p := range imagePaths {
		if c := AttachmentChip(p); c != "" {
			chips = append(chips, c)
		}
	}
	return strings.Join(chips, " ")
}

// InputStatusLine builds row 2 of the input box: color-coded segments
// joined by " · " per spec:
//
//	[mode, blue] · [model name, white][provider, dim gray] · [highlight, amber+bold]
//
// It folds the old "Type a task..." / "native · no model installed" plain line
// into this single pattern. Empty segments are skipped.
func InputStatusLine(mode, modelName, provider, highlight string) string {
	return InputStatusLinesWithImages(mode, modelName, provider, highlight, nil)
}

// InputStatusLineWithImage is like InputStatusLine but prepends an attachment chip
// when imagePath is non-empty, producing e.g. "[🖼 photo.png] · ollama · qwen2-vl".
func InputStatusLineWithImage(mode, modelName, provider, highlight, imagePath string) string {
	var paths []string
	if strings.TrimSpace(imagePath) != "" {
		paths = []string{imagePath}
	}
	return InputStatusLinesWithImages(mode, modelName, provider, highlight, paths)
}

// InputStatusLinesWithImages is like InputStatusLine but prepends one attachment
// chip per attached image, producing e.g. "[🖼 a.png] [🖼 b.png] · ollama · qwen2-vl".
func InputStatusLinesWithImages(mode, modelName, provider, highlight string, imagePaths []string) string {
	var segs []string
	if chips := AttachmentChips(imagePaths); chips != "" {
		segs = append(segs, chips)
	}
	if mode != "" {
		segs = append(segs, Blue(mode))
	}
	if modelName != "" {
		m := White(modelName)
		if provider != "" {
			// provider in dim-gray, e.g. " (ollama)" or " · ollama"
			m += Muted(" (" + provider + ")")
		}
		segs = append(segs, m)
	} else if provider != "" {
		segs = append(segs, Muted(provider))
	} else {
		// no model name and no provider: surface "no model installed" as white
		// segment when a mode is present (welcome preview for no-model state)
		// or as part of the full fallback when even mode is missing.
		if mode != "" && highlight == "" {
			segs = append(segs, White("no model installed"))
		} else if mode == "" && highlight == "" {
			// will be handled by fallback below
		}
	}
	if highlight != "" {
		segs = append(segs, AmberBold(highlight))
	}
	if len(segs) == 0 {
		// fallback for welcome preview when nothing provided — mirrors old behavior
		segs = append(segs, Blue("native"), White("no model installed"))
	}
	if len(segs) == 1 {
		// single segment (e.g. only mode or only highlight) — no separator needed
		return segs[0]
	}
	// join with muted middle dot
	sep := Muted(" · ")
	return segs[0] + sep + joinWithSep(segs[1:], sep)
}

// joinWithSep joins already-styled segments with sep.
func joinWithSep(segs []string, sep string) string {
	if len(segs) == 0 {
		return ""
	}
	out := segs[0]
	for _, s := range segs[1:] {
		out += sep + s
	}
	return out
}

// InputBoxTwoRow renders the welcome screen's rounded, bordered input box
// with two rows per spec: placeholder on row 1, status segments on row 2.
// Width is the outer box width (including borders); inner content width is width-4
// (one border + two spaces padding on each side).
func InputBoxTwoRow(width int, mode, modelName, provider, highlight string) string {
	return InputBoxTwoRowWithImages(width, mode, modelName, provider, highlight, nil)
}

// InputBoxTwoRowWithImage is like InputBoxTwoRow but includes an attachment chip when imagePath is set.
func InputBoxTwoRowWithImage(width int, mode, modelName, provider, highlight, imagePath string) string {
	var paths []string
	if strings.TrimSpace(imagePath) != "" {
		paths = []string{imagePath}
	}
	return InputBoxTwoRowWithImages(width, mode, modelName, provider, highlight, paths)
}

// InputBoxTwoRowWithImages is like InputBoxTwoRow but includes one attachment
// chip per attached image in the status row.
func InputBoxTwoRowWithImages(width int, mode, modelName, provider, highlight string, imagePaths []string) string {
	if width < 24 {
		width = 24
	}
	inner := width - 4 // between borders, excluding "│ " and " │"
	if inner < 10 {
		inner = 10
	}
	// Row 1: cursor + placeholder (faded). Example task in quotes dimmed.
	row1Plain := "› " + placeholderAsk + " " + placeholderExample
	// Styled version for rendering
	row1Styled := Blue("›") + " " + Muted(placeholderAsk) + " " + Muted(placeholderExample)
	// Truncate if needed
	if visibleWidth(row1Plain) > inner {
		// keep cursor, truncate placeholder
		avail := inner - 2 // for "› "
		if avail < 4 {
			avail = 4
		}
		ph := truncateVisible(Muted(placeholderAsk+" "+placeholderExample), avail)
		row1Styled = Blue("›") + " " + ph
		row1Plain = "› " + stripANSI(ph)
	}
	pad1 := inner - visibleWidth(row1Plain)
	if pad1 < 0 {
		pad1 = 0
	}
	padStr1 := strings.Repeat(" ", pad1)
	if GetBackgroundEscape() != "" {
		padStr1 = Background(padStr1)
	}

	status := InputStatusLinesWithImages(mode, modelName, provider, highlight, imagePaths)
	// Plain width for padding: stripANSI then count
	statusPlain := stripANSI(status)
	// If status too long, truncate with style preserved (truncateVisible handles ANSI)
	if visibleWidth(status) > inner {
		status = truncateVisible(status, inner)
		statusPlain = stripANSI(status)
	}
	pad2 := inner - visibleWidth(statusPlain)
	if pad2 < 0 {
		pad2 = 0
	}
	padStr2 := strings.Repeat(" ", pad2)
	if GetBackgroundEscape() != "" {
		padStr2 = Background(padStr2)
	}

	var b strings.Builder
	b.WriteString(InputBoxTop(width))
	b.WriteByte('\n')
	// Row 1 — borders are styled (include bg via style), inner content styled, pad has bg
	b.WriteString(Border("│") + " " + row1Styled + padStr1 + " " + Border("│"))
	b.WriteByte('\n')
	b.WriteString(Border("│") + " " + status + padStr2 + " " + Border("│"))
	b.WriteByte('\n')
	b.WriteString(InputBoxBottom(width))
	return b.String()
}
