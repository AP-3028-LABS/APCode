//go:build windows

package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowsPlatformUsesNativePicker verifies that on Windows /image
// resolves to the native-dialog picker implementation.
func TestWindowsPlatformUsesNativePicker(t *testing.T) {
	r := &REPL{}
	p := newImagePicker(r)
	if _, ok := p.(*windowsImagePicker); !ok {
		t.Fatalf("newImagePicker on windows must return *windowsImagePicker, got %T", p)
	}
}

// TestNativeDialogSelection verifies a path chosen in the native dialog is
// returned through the interface (injected dialog, no GUI in CI).
func TestNativeDialogSelection(t *testing.T) {
	oldDialog, oldGate := runWindowsImageDialog, nativeImagePickerEnabled
	runWindowsImageDialog = func(string) (string, error) {
		return `C:\Users\test\Pictures\screenshot.png`, nil
	}
	nativeImagePickerEnabled = func(*REPL) bool { return true }
	defer func() {
		runWindowsImageDialog, nativeImagePickerEnabled = oldDialog, oldGate
	}()

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	got, err := newImagePicker(repl).PickImage("C:\\Users\\test\\Docs")
	if err != nil {
		t.Fatalf("PickImage: %v", err)
	}
	if got != filepath.Clean(`C:\Users\test\Pictures\screenshot.png`) {
		t.Fatalf("expected chosen path, got %q", got)
	}
}

// TestNativeDialogCancel verifies cancel in the native dialog is silent:
// ("", nil), no error, no terminal picker, no attachment.
func TestNativeDialogCancel(t *testing.T) {
	oldDialog, oldGate := runWindowsImageDialog, nativeImagePickerEnabled
	runWindowsImageDialog = func(string) (string, error) { return "", nil }
	nativeImagePickerEnabled = func(*REPL) bool { return true }
	defer func() {
		runWindowsImageDialog, nativeImagePickerEnabled = oldDialog, oldGate
	}()

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	got, err := newImagePicker(repl).PickImage("C:\\Users\\test")
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

// TestNativeDialogFailureFallsBackToTerminal verifies the deterministic
// fallback: when the native dialog cannot launch, /image falls back to the
// terminal picker instead of failing the command.
func TestNativeDialogFailureFallsBackToTerminal(t *testing.T) {
	oldDialog, oldGate := runWindowsImageDialog, nativeImagePickerEnabled
	runWindowsImageDialog = func(string) (string, error) { return "", errors.New("dialog failed") }
	nativeImagePickerEnabled = func(*REPL) bool { return true }
	defer func() {
		runWindowsImageDialog, nativeImagePickerEnabled = oldDialog, oldGate
	}()

	tmp := t.TempDir()
	img := filepath.Join(tmp, "screenshot.png")
	writeTestPNG(t, img)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("1\n"), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	got, err := newImagePicker(repl).PickImage(tmp)
	if err != nil {
		t.Fatalf("fallback must not error, got %v", err)
	}
	if got != img {
		t.Fatalf("expected fallback selection %q, got %q", img, got)
	}
	if !strings.Contains(out.String(), "switching to terminal picker") {
		t.Errorf("expected fallback notice, got:\n%s", out.String())
	}
}

// TestScriptedInputUsesTerminalPicker verifies that when stdin is not a
// console (piped/scripted input, CI), the native dialog is NOT launched and
// the terminal picker is used instead. This keeps `go test ./...` on
// windows-latest deterministic without opening a GUI.
func TestScriptedInputUsesTerminalPicker(t *testing.T) {
	tmp := t.TempDir()
	img := filepath.Join(tmp, "screenshot.png")
	writeTestPNG(t, img)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("1\n"), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	// Default gate: buffer stdin means not a terminal -> terminal picker.
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

// TestStdinIsNotTerminalForBuffers guards the non-TTY detection helper used
// to avoid popping a GUI dialog during scripted input and CI.
func TestStdinIsNotTerminalForBuffers(t *testing.T) {
	repl, err := NewREPL(bytes.NewBufferString(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	if repl.stdinIsTerminal() {
		t.Error("a bytes.Buffer stdin must not be treated as a terminal")
	}
}
