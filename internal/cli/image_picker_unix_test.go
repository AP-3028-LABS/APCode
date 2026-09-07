//go:build !windows

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnixPlatformUsesTerminalPicker verifies that on macOS/Linux /image
// resolves to the existing terminal line-based picker (no GUI dependency).
func TestUnixPlatformUsesTerminalPicker(t *testing.T) {
	r := &REPL{}
	p := newImagePicker(r)
	_, ok := p.(terminalImagePicker)
	if !ok {
		t.Fatalf("newImagePicker on !windows must return terminalImagePicker, got %T", p)
	}
}

// TestTerminalPickerCancel verifies cancel semantics of the interface:
// ("", nil), no error, nothing attached.
func TestTerminalPickerCancel(t *testing.T) {
	tmp := t.TempDir()
	repl, err := NewREPL(bytes.NewBufferString("\x1b\n"), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	got, err := newImagePicker(repl).PickImage(tmp)
	if err != nil {
		t.Fatalf("cancel must not error, got %v", err)
	}
	if got != "" {
		t.Fatalf("cancel must return empty path, got %q", got)
	}
	if len(repl.Attachments) != 0 {
		t.Fatalf("cancel must not attach anything, got %d", len(repl.Attachments))
	}
}

// TestTerminalPickerValidSelection verifies the interface returns the
// selected absolute path for a valid image.
func TestTerminalPickerValidSelection(t *testing.T) {
	tmp := t.TempDir()
	img := filepath.Join(tmp, "screenshot.png")
	writeTestPNG(t, img)
	os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte("hi"), 0o644)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("1\n"), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	got, err := newImagePicker(repl).PickImage(tmp)
	if err != nil {
		t.Fatalf("PickImage: %v", err)
	}
	if got != img {
		t.Fatalf("expected %q, got %q", img, got)
	}
	if !strings.Contains(out.String(), "Image attachment picker") {
		t.Errorf("expected terminal picker to render, got:\n%s", out.String())
	}
}
