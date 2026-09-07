//go:build windows

package cli

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"apcode/internal/tui"
)

// windowsImagePicker selects an image using the native Windows "Open File"
// dialog (System.Windows.Forms.OpenFileDialog driven through in-box
// PowerShell). When stdin is not attached to an interactive console
// (scripted input, tests, CI) or the native dialog cannot be launched, it
// falls back to the terminal line-based picker so /image never becomes
// unusable. The chosen path is validated later by the existing vision
// layer; no file is modified, moved, or deleted.
type windowsImagePicker struct {
	repl *REPL
}

// runWindowsImageDialog launches the native Windows file dialog and returns
// the chosen absolute path, or "" when the user cancels. It is a package
// variable so windows-specific tests can inject deterministic results.
var runWindowsImageDialog = func(startDir string) (string, error) {
	return launchWindowsOpenFileDialog(startDir)
}

// nativeImagePickerEnabled reports whether the native dialog should be used
// for the given REPL. It is swappable so tests can force either branch.
var nativeImagePickerEnabled = func(repl *REPL) bool { return repl.stdinIsTerminal() }

// newImagePicker returns the platform default picker for this REPL.
func newImagePicker(r *REPL) ImagePicker {
	return &windowsImagePicker{repl: r}
}

func (p *windowsImagePicker) PickImage(startDir string) (string, error) {
	if nativeImagePickerEnabled(p.repl) {
		picked, err := runWindowsImageDialog(startDir)
		if err == nil {
			if picked == "" {
				// Cancelled in the native dialog: no error, no attachment.
				return "", nil
			}
			return filepath.Clean(picked), nil
		}
		fmt.Fprintf(p.repl.Out, "%s Native image picker unavailable (%v), switching to terminal picker...\n", tui.Warning("⚠"), err)
	}
	// Scripted input or dialog failure: deterministic terminal picker fallback.
	return p.repl.pickImageFile(startDir), nil
}

// launchWindowsOpenFileDialog runs the in-box PowerShell OpenFileDialog
// (STA) via -EncodedCommand so no temp script file is needed and no console
// window is flashed. The dialog is the standard native Windows file picker:
// it supports normal navigation, filename entry, locations outside the
// workspace, system cancel behavior, and filters to the supported image
// formats. PowerShell prints only the selected filename on OK, and nothing
// on cancel.
func launchWindowsOpenFileDialog(startDir string) (string, error) {
	return runEncodedPowerShell(buildWindowsOpenFileDialogScript(startDir))
}

// runEncodedPowerShell executes a UTF-16LE/base64-encoded PowerShell script
// with the exact flags APCode relies on (-NoProfile, -NonInteractive,
// -WindowStyle Hidden, -ExecutionPolicy Bypass, -STA) and returns the
// trimmed stdout (with BOM stripped). Empty output reports as "".
func runEncodedPowerShell(script string) (string, error) {
	encoded := base64.StdEncoding.EncodeToString(utf16LEBytes(script))
	out, err := exec.Command("powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass",
		"-STA",
		"-EncodedCommand", encoded,
	).Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	if strings.HasPrefix(line, "\ufeff") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	}
	return line, nil
}

// buildWindowsOpenFileDialogScript produces the PowerShell that shows the
// OpenFileDialog. The filter covers exactly the APCode-supported formats;
// final magic-byte/extension validation still runs in the vision layer.
func buildWindowsOpenFileDialogScript(startDir string) string {
	dir := strings.TrimSpace(startDir)
	initialDir := ""
	if dir != "" {
		escaped := strings.ReplaceAll(dir, "'", "''")
		initialDir = "if ([System.IO.Directory]::Exists('" + escaped + "')) { $d.InitialDirectory='" + escaped + "' }"
	}
	return "$ErrorActionPreference='Stop'\n" +
		"Add-Type -AssemblyName System.Windows.Forms\n" +
		"$d=New-Object System.Windows.Forms.OpenFileDialog\n" +
		"$d.Title='APCode - Select image'\n" +
		"$d.Filter='Image files (*.png;*.jpg;*.jpeg;*.webp;*.gif;*.bmp)|*.png;*.jpg;*.jpeg;*.webp;*.gif;*.bmp'\n" +
		"$d.CheckFileExists=$true\n" +
		"$d.ValidateNames=$true\n" +
		"$d.RestoreDirectory=$true\n" +
		"$d.Multiselect=$false\n" +
		initialDir + "\n" +
		"$res=$d.ShowDialog()\n" +
		"if ($res -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Write($d.FileName) }\n"
}

// utf16LEBytes encodes s as UTF-16LE for PowerShell -EncodedCommand.
func utf16LEBytes(s string) []byte {
	units := utf16.Encode([]rune(s))
	b := make([]byte, 0, len(units)*2)
	for _, u := range units {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}
