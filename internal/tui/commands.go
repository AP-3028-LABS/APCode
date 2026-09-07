package tui

import (
	"strings"
)

// Command is one entry in the APCode command menu. Only commands backed by
// a real handler should ever be added here.
type Command struct {
	Name        string // e.g. "/help"
	Description string // e.g. "show help"
	Shortcut    string // optional keyboard hint, e.g. "Ctrl+C"; empty = none
}

// DefaultMenuCommands returns the commands advertised on the welcome
// screen. The REPL passes its own list when it differs; this exists so the
// menu has one obvious home and is easy to extend.
func DefaultMenuCommands() []Command {
	return []Command{
		{Name: "/help", Description: "show help"},
		{Name: "/new", Description: "new session"},
		{Name: "/models", Description: "list models"},
		{Name: "/status", Description: "system status"},
		{Name: "/tools", Description: "list agent tools"},
		{Name: "/context", Description: "project context"},
		{Name: "/image", Description: "attach image(s) via picker"},
		{Name: "/exit", Description: "exit APCode"},
	}
}

// CommandMenu renders the command shortcut list:
//
//	/help       show help                Ctrl+C
//	/models     list models
//
// Every line is clamped to width columns.
func CommandMenu(cmds []Command, width int) string {
	const indent = "  "
	nameW := 0
	for _, c := range cmds {
		if len(c.Name) > nameW {
			nameW = len(c.Name)
		}
	}
	nameW += 2

	var b strings.Builder
	for _, c := range cmds {
		line := indent + Primary(padRight(c.Name, nameW)) + Muted(c.Description)
		if c.Shortcut != "" {
			gap := width - visibleWidth(line) - len(c.Shortcut) - 1
			if gap > 0 {
				line += strings.Repeat(" ", gap)
			} else {
				line += "  "
			}
			line += Muted(c.Shortcut)
		}
		b.WriteString(truncateVisible(line, width))
		b.WriteByte('\n')
	}
	return b.String()
}

// padRight pads s with spaces to exactly n visible columns.
func padRight(s string, n int) string {
	w := visibleWidth(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// truncateVisible cuts s to at most width visible columns without breaking
// ANSI sequences mid-way (styling of the kept prefix is preserved as-is;
// Reset terminates any open sequence).
func truncateVisible(s string, width int) string {
	if visibleWidth(s) <= width {
		return s
	}
	var b strings.Builder
	col := 0
	inSeq := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inSeq = true
			b.WriteRune(r)
		case inSeq:
			b.WriteRune(r)
			if r == 'm' {
				inSeq = false
			}
		default:
			if col >= width-1 {
				b.WriteString(Reset())
				return b.String()
			}
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// FooterHints renders a left/right aligned hint line under the input box:
//
//	Enter ↵ send                         Local · Phi-3 Mini Q4
//
// It never exceeds width columns; the right hint is dropped if both do
// not fit.
func FooterHints(left, right string, width int) string {
	lw := visibleWidth(left)
	rw := 0
	if right != "" {
		rw = visibleWidth(right)
	}
	if lw+rw+4 > width {
		right = ""
		rw = 0
	}
	line := "  " + Muted(left)
	if right != "" {
		gap := width - lw - rw - 2
		if gap > 0 {
			line += strings.Repeat(" ", gap)
		} else {
			line += "  "
		}
		line += Accent(right)
	}
	return line
}
