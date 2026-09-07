package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// compactLogo is the small APCode mark used on narrow terminals.
const compactLogo = ` █▀▀ █▀▀█ █▀▀▄ █▀▀ █▀▀ █▀▀▄
 █   █  █ █  █ █▀▀ ▀▀█ █▀▀▄
 ▀▀▀ ▀▀▀▀ ▀  ▀ ▀▀▀ ▀▀▀ ▀▀▀ `

// WelcomeOptions describes everything the welcome screen needs. All fields
// are produced from real state by the caller; the renderer never guesses.
// Commands is kept for backward compat but is intentionally ignored on the
// welcome screen — the static list is moved to the command palette per
// OpenCode-inspired design (discovery via ctrl+p / /help).
type WelcomeOptions struct {
	Version     string
	Commands    []Command // kept for compat; not rendered on welcome
	ProjectLine string    // e.g. "Go · 42 files · Git: main"; may be empty
	Width       int       // terminal width; <=0 means default (80)
	Height      int       // terminal height; 0 = auto (no fill). When set, fills whole viewport with background.

	// New OpenCode-inspired fields — all optional with sensible defaults.
	Mode      string // blue segment, e.g. "native" or "plan"; defaults to "native"
	ModelName string // white segment, e.g. "Phi-3 Mini Q4"; empty => "no model installed"
	Provider  string // dim-gray suffix, e.g. "ollama"; optional
	Highlight string // amber+bold segment, e.g. "reasoning: high"; optional
	HasModel  bool   // drives context-aware tip; false => tip about /models

	Workspace string // for status bar, e.g. "~/APCode" or absolute; optional
	GitBranch string // for status bar; optional
	TipText   string // override tip sentence; empty => auto

	AttachedImage  string   // optional image attachment path to show chip, e.g. "/tmp/photo.png" (kept for compat)
	AttachedImages []string // optional multiple image attachment paths to show chips
}

// WelcomeScreen renders the full startup screen in the OpenCode-inspired
// layout — narrow palette, woven logo, two-row input box, keybind hints,
// tip, and pinned status bar. It never exceeds the terminal width and
// automatically switches to a compact logo below 80 columns.
func WelcomeScreen(o WelcomeOptions) string {
	width := o.Width
	if width <= 0 {
		width = defaultTermWidth
	}
	mode := LayoutForWidth(width)
	var b strings.Builder

	// 1. Woven logo — keep APCode's own blocky wordmark, cycle white→mid-gray→dim-gray
	rawLogo := logo
	if mode == LayoutCompact {
		rawLogo = compactLogo
	}
	woven := wovenLogo(rawLogo)
	for _, line := range strings.Split(woven, "\n") {
		b.WriteString(center(line, width))
		b.WriteByte('\n')
	}
	// 2. Version (muted), then repo summary (muted) — both centered, muted hierarchy
	b.WriteString(center(Muted("v"+o.Version), width))
	b.WriteByte('\n')

	if o.ProjectLine != "" {
		// spec: keep repo summary just restyled to muted-gray, centered
		b.WriteByte('\n')
		pl := truncateVisible(Muted(o.ProjectLine), width)
		b.WriteString(center(pl, width))
		b.WriteByte('\n')
	}

	// 3. Rounded bordered input box with TWO rows (centered)
	b.WriteByte('\n')
	boxW := BoxWidth(width)
	modeVal := o.Mode
	if modeVal == "" {
		modeVal = "native"
	}
	// Fold old plain lines into the status row pattern; old single-image field
	// is folded into the multi-image list for compatibility.
	imagePaths := o.AttachedImages
	if strings.TrimSpace(o.AttachedImage) != "" {
		imagePaths = append([]string{o.AttachedImage}, imagePaths...)
	}
	boxStr := InputBoxTwoRowWithImages(boxW, modeVal, o.ModelName, o.Provider, o.Highlight, imagePaths)
	for _, line := range strings.Split(boxStr, "\n") {
		if line == "" {
			continue
		}
		// center the box as a block
		left := (width - visibleWidth(line)) / 2
		if left < 0 {
			left = 0
		}
		b.WriteString(strings.Repeat(" ", left))
		b.WriteString(line)
		b.WriteByte('\n')
	}

	// 4. Below the box, unbordered: centered row of keybind hints (keys brighter than desc)
	b.WriteByte('\n')
	hints := KeybindHints(width)
	b.WriteString(centerHints(hints, width))
	b.WriteByte('\n')

	// 5. One tip line: amber dot + bold amber "Tip" + muted sentence, command bolded/white
	b.WriteByte('\n')
	tip := renderTip(o, width)
	b.WriteString(center(tip, width))
	b.WriteByte('\n')

	// 6. Persistent status bar pinned to bottom: dir:branch left, version right, both dim gray
	// Always render if we have version or workspace; it is the last line so it sits at bottom.
	bar := StatusBar(width, o.Workspace, o.GitBranch, o.Version)
	if visibleWidth(bar) > 0 {
		b.WriteByte('\n')
		b.WriteString(bar)
		b.WriteByte('\n')
	}

	raw := b.String()
	// Apply dynamic background fill across the entire viewport so the welcome
	// screen shows an opencode-like background even without user config.
	// Every row (main viewport, input boxes, sidebars) shares the same fill.
	if GetBackgroundEscape() != "" && ColorsEnabled() {
		lines := strings.Split(raw, "\n")
		for i, l := range lines {
			if i == len(lines)-1 && l == "" {
				continue
			}
			lines[i] = BackgroundFill(l, width)
		}
		raw = strings.Join(lines, "\n")
	}
	// Fill whole terminal background when Height is known (apcode REPL).
	// This makes `apcode` turn the entire terminal canvas opencode-like,
	// not just the printed rows.
	if o.Height > 0 && GetBackgroundEscape() != "" && ColorsEnabled() {
		lines := strings.Split(raw, "\n")
		// effective lines excludes trailing "" from final "\n"
		eff := len(lines)
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			eff--
		}
		if eff < o.Height {
			bgLine := BackgroundFill("", width)
			// insert extra background-filled rows before trailing newline
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			for i := 0; i < o.Height-eff; i++ {
				lines = append(lines, bgLine)
			}
			lines = append(lines, "")
			raw = strings.Join(lines, "\n")
		}
	}
	return raw
}

// wovenLogo renders the APCODE block wordmark with AP in green and CODE in white
// per user request: "AP" green, "CODE" white. AP = first ~1/3 of the line width,
// CODE = remaining ~2/3. Uses column position rather than group counting so
// adjacent block letters (no spaces) are still split correctly.
func wovenLogo(s string) string {
	var out strings.Builder
	lines := strings.Split(s, "\n")
	for li, line := range lines {
		if li > 0 {
			out.WriteByte('\n')
		}
		totalW := visibleWidth(line)
		// AP occupies ~35% of the line (2 of 6 letters), CODE the rest
		splitAt := totalW * 35 / 100
		if splitAt < 8 {
			splitAt = totalW * 2 / 6
		}
		col := 0
		for _, r := range line {
			if r == '\n' || r == '\r' {
				out.WriteRune(r)
				continue
			}
			if r == ' ' || r == '\t' {
				out.WriteRune(r)
				col++
				continue
			}
			// Color by column: AP green, CODE white
			if col < splitAt {
				out.WriteString(Success(string(r)))
			} else {
				out.WriteString(White(string(r)))
			}
			col++
		}
	}
	return out.String()
}

// APCodeStyled returns the wordmark "APCode" with AP in green and Code in white.
// Use this for any textual "APCode" mentions to keep branding consistent.
func APCodeStyled() string {
	return Success("AP") + White("Code")
}

// center pads s so it appears horizontally centered within width columns,
// ignoring ANSI styling. Oversized content is returned unchanged.
func center(s string, width int) string {
	w := visibleWidth(s)
	if w >= width {
		return s
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + s
}

// centerHints is like center but trims trailing spaces after centering for hints
func centerHints(s string, width int) string {
	w := visibleWidth(s)
	if w >= width {
		return truncateVisible(s, width)
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + s
}

// KeybindHints renders one unbordered centered row of keybind hints with
// generous spacing between pairs, keys slightly brighter than descriptions.
// Maps to APCode's real bindings (enter/cancel/commands) — not OpenCode's.
func KeybindHints(width int) string {
	// APCode's real bindings: Enter to send, Ctrl+C to cancel, Ctrl+P for commands
	pairs := []struct{ key, desc string }{
		{key: "enter", desc: "send"},
		{key: "ctrl+c", desc: "cancel"},
		{key: "ctrl+p", desc: "commands"},
		// keep tab agents as familiar hint if provider supports agents
		{key: "tab", desc: "agents"},
	}
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			// generous spacing between pairs per spec
			b.WriteString(strings.Repeat(" ", 6))
		}
		// key slightly brighter than desc: key white, desc mid-gray
		b.WriteString(White(p.key))
		b.WriteString(" ")
		b.WriteString(Secondary(p.desc))
	}
	line := b.String()
	if visibleWidth(line) > width {
		// on narrow terminals, drop to fewer pairs rather than overflow
		// keep first 3 pairs then 2 etc
		for keep := len(pairs) - 1; keep >= 2; keep-- {
			var tmp strings.Builder
			for i := 0; i < keep; i++ {
				if i > 0 {
					tmp.WriteString(strings.Repeat(" ", 6))
				}
				tmp.WriteString(White(pairs[i].key))
				tmp.WriteString(" ")
				tmp.WriteString(Secondary(pairs[i].desc))
			}
			if visibleWidth(tmp.String()) <= width {
				return tmp.String()
			}
		}
		return truncateVisible(line, width)
	}
	return line
}

// renderTip builds the amber dot + bold amber Tip + muted sentence,
// with any command name bolded/white. Context-aware: if HasModel is false
// (APCode's actual current state), surface a tip about /models.
func renderTip(o WelcomeOptions, width int) string {
	sentence := o.TipText
	if sentence == "" {
		if !o.HasModel {
			sentence = "Run /models to install a local model — APCode works fully offline."
		} else {
			sentence = "Configure local or remote MCP servers in the mcp config section."
		}
	}
	// Build tip: "● Tip <sentence>" where Tip is bold amber, dot amber, sentence muted with commands white
	// Fill dot: use amber bullet
	dot := Amber("●")
	label := AmberBold("Tip")
	// Bold any /command tokens inside sentence as white
	sentenceStyled := styleCommandsInSentence(sentence)
	tip := dot + " " + label + " " + sentenceStyled
	// Truncate to width if needed
	if visibleWidth(tip) > width {
		tip = truncateVisible(tip, width)
	}
	return tip
}

// styleCommandsInSentence bolds/whitens any "/word" tokens for emphasis.
func styleCommandsInSentence(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		// detect /command at start of token (strip trailing punctuation)
		trim := strings.TrimRight(w, ".,;:!?—")
		if strings.HasPrefix(trim, "/") && len(trim) > 1 {
			suffix := w[len(trim):]
			words[i] = White(trim) + Muted(suffix)
		} else {
			words[i] = Muted(w)
		}
		if i > 0 {
			// preserve single spaces
			words[i] = " " + words[i]
		}
	}
	// words[0] already has no leading space; join manually
	var b strings.Builder
	for i, w := range words {
		if i == 0 {
			b.WriteString(strings.TrimPrefix(w, " "))
		} else {
			b.WriteString(w)
		}
	}
	// The above loop added spaces via prefix; simplify: join with space using Muted already?
	// Actually we handled spaces, so just return built
	return b.String()
}

// StatusBar renders the persistent bottom bar: dir:branch left, version right, both dim gray.
// dir is shortened with ~ for home.
func StatusBar(width int, workspace, branch, version string) string {
	if width <= 0 {
		width = defaultTermWidth
	}
	left := ""
	if workspace != "" {
		dir := shortenHome(workspace)
		left = dir
		if branch != "" {
			left += ":" + branch
		}
	} else if branch != "" {
		left = branch
	}
	right := ""
	if version != "" {
		right = "v" + version
	}
	// Both dim gray
	leftStyled := ""
	if left != "" {
		leftStyled = Muted(left)
	}
	rightStyled := ""
	if right != "" {
		rightStyled = Muted(right)
	}
	lw := visibleWidth(leftStyled)
	rw := visibleWidth(rightStyled)
	if lw == 0 && rw == 0 {
		return ""
	}
	if lw+rw+2 > width {
		// not enough room for both; prioritize left, drop right if needed
		if lw <= width {
			return truncateVisible(leftStyled, width)
		}
		return truncateVisible(leftStyled, width)
	}
	// left aligned, right aligned
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	return leftStyled + strings.Repeat(" ", gap) + rightStyled
}

// shortenHome replaces the home directory prefix with ~ for display.
func shortenHome(p string) string {
	if p == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
