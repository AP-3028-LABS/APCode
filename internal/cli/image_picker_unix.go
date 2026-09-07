//go:build !windows

package cli

// terminalImagePicker is the macOS/Linux image picker. No desktop environment
// or GUI toolkit is required: /image renders the existing numbered, line-based
// picker in the terminal.
//
// The shared implementation lives in image_picker.go (terminalImagePicker).
// This file only wires platform selection to it.
func newImagePicker(r *REPL) ImagePicker {
	return terminalImagePicker{repl: r}
}
