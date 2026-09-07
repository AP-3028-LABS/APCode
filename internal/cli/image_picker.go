package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"apcode/internal/tui"
	"apcode/internal/vision"
)

// ImageAttachment is one image attached to the composer via /image. It carries
// the file path plus metadata used to build the multimodal request payload.
type ImageAttachment struct {
	Path     string
	Filename string
	Mime     string
	Size     int64
}

// pickerEntry is one selectable row in the image file picker.
type pickerEntry struct {
	index   int
	name    string
	full    string
	isDir   bool
	isImage bool
}

// ImagePicker selects an image file for attachment behind a platform-neutral
// interface. Implementations return the absolute path of the chosen file, or
// ("", nil) when the user cancels without selecting. Implementations only
// read the selected file and must never modify, move, or delete it. The image
// backend, attachment state, and multimodal pipeline are untouched by the
// picker; the caller attaches whatever path comes back.
//
//   - Windows: a native Windows "Open File" dialog, falling back to the
//     terminal picker when stdin is not an interactive console or the dialog
//     cannot be launched.
//   - macOS/Linux: the terminal line-based picker (no desktop requirement).
type ImagePicker interface {
	PickImage(startDir string) (string, error)
}

// terminalImagePicker implements ImagePicker on top of the existing line-based
// picker. It is the macOS/Linux default and the fallback for scripted-input
// Windows sessions (piped stdin, tests, CI) where a GUI dialog is not usable.
type terminalImagePicker struct {
	repl *REPL
}

// PickImage runs the interactive line-based picker starting at startDir.
// It returns the selected absolute path, or "" when the user cancels.
func (p terminalImagePicker) PickImage(startDir string) (string, error) {
	return p.repl.pickImageFile(startDir), nil
}

// pickImageFile runs an interactive, line-oriented image picker starting at
// startDir. It returns the absolute path of the chosen image file, or "" when
// the user cancels (Esc, empty line, q/quit). It is line oriented on purpose
// so it works in cooked-mode terminals with no raw-mode/TUI dependency.
func (r *REPL) pickImageFile(startDir string) string {
	dir, err := filepath.Abs(startDir)
	if err != nil || dir == "" {
		dir, _ = os.Getwd()
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir, _ = os.Getwd()
	}

	for {
		entries := imagePickerEntries(dir)
		var b strings.Builder
		b.WriteString(tui.Primary("Image attachment picker"))
		b.WriteString(tui.Muted("  —  number: select · path: open · ..: up · empty: cancel"))
		b.WriteString("\n")
		b.WriteString(tui.Muted("  " + dir))
		b.WriteString("\n")
		for _, e := range entries {
			label := e.name
			if e.isDir {
				label = tui.Blue(e.name + "/")
			} else if e.isImage {
				label = vision.AttachmentChip(e.name)
			}
			fmt.Fprintf(&b, "  %2d  %s\n", e.index, label)
		}
		fmt.Fprint(r.Out, b.String())

		fmt.Fprint(r.Out, tui.Muted("Choose [number|path|..|Enter=cancel]: "))
		line, err := r.readPictureLine()
		if err != nil {
			return ""
		}
		sel := strings.TrimSpace(strings.ReplaceAll(line, "\x1b", ""))
		lower := strings.ToLower(sel)
		if sel == "" || lower == "esc" || lower == "exit" || lower == "q" || lower == "quit" {
			fmt.Fprintln(r.Out, tui.Muted("Image selection cancelled."))
			return ""
		}
		if lower == ".." || lower == "up" || lower == "back" || lower == "p" {
			if parent := filepath.Dir(dir); parent != dir {
				dir = parent
			}
			continue
		}
		if n, err2 := strconv.Atoi(sel); err2 == nil {
			var target *pickerEntry
			for i := range entries {
				if entries[i].index == n {
					target = &entries[i]
					break
				}
			}
			if target == nil {
				fmt.Fprintln(r.Out, tui.Warning("No such entry. Pick a listed number, a path, or press Enter to cancel."))
				continue
			}
			if target.isDir {
				dir = target.full
				continue
			}
			return target.full
		}
		// Treat the selection as a path, relative to the current directory.
		raw := sel
		if len(raw) >= 2 && ((raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'')) {
			raw = raw[1 : len(raw)-1]
		}
		candidate := raw
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(dir, candidate)
		}
		candidate = filepath.Clean(candidate)
		info, err := os.Stat(candidate)
		if err != nil {
			fmt.Fprintf(r.Out, "%s File not found: %s\n", tui.Error("✗"), raw)
			continue
		}
		if info.IsDir() {
			dir = candidate
			continue
		}
		if verr := vision.ValidateImageFile(candidate); verr != nil {
			fmt.Fprintf(r.Out, "%s %v\n", tui.Error("✗"), verr)
			continue
		}
		return candidate
	}
}

// imagePickerEntries lists directories and supported image files under dir,
// directories first (case-insensitive sort), then images, both 1-indexed.
func imagePickerEntries(dir string) []pickerEntry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs, images []pickerEntry
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, pickerEntry{name: e.Name(), full: filepath.Join(dir, e.Name()), isDir: true})
			continue
		}
		if vision.IsSupportedExtension(filepath.Ext(e.Name())) {
			images = append(images, pickerEntry{name: e.Name(), full: filepath.Join(dir, e.Name()), isImage: true})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name) })
	sort.Slice(images, func(i, j int) bool { return strings.ToLower(images[i].name) < strings.ToLower(images[j].name) })

	out := make([]pickerEntry, 0, len(dirs)+len(images))
	idx := 1
	for i := range dirs {
		dirs[i].index = idx
		out = append(out, dirs[i])
		idx++
	}
	for i := range images {
		images[i].index = idx
		out = append(out, images[i])
		idx++
	}
	return out
}

// readPictureLine reads one interactive picker line from the REPL input.
func (r *REPL) readPictureLine() (string, error) {
	if r.reader == nil {
		if r.In == nil {
			return "", io.EOF
		}
		r.reader = bufio.NewReader(r.In)
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return line, nil
}

// stdinIsTerminal reports whether the REPL input is attached to an
// interactive character device (a real terminal). Piped stdin, redirected
// input, scripted feed, and test buffers are not terminals, so interactive
// only features such as the native Windows file dialog fall back to the
// line-based picker when stdin is not a console.
func (r *REPL) stdinIsTerminal() bool {
	f, ok := r.In.(*os.File)
	if !ok || f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
