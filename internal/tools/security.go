package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// CommandClass is the security classification of a terminal command.
type CommandClass int

const (
	// ClassSafe commands are read-only or standard validation commands that
	// cannot modify the project or system. They may run without user approval.
	ClassSafe CommandClass = iota
	// ClassApprovalRequired commands may modify the project, install software,
	// delete data, or otherwise have side effects. They always require
	// explicit user approval before execution.
	ClassApprovalRequired
	// ClassBlocked commands are destructive at the system level and are never
	// executed by APCode, regardless of approval.
	ClassBlocked
)

// String implements fmt.Stringer.
func (c CommandClass) String() string {
	switch c {
	case ClassSafe:
		return "SAFE"
	case ClassApprovalRequired:
		return "REQUIRES_APPROVAL"
	case ClassBlocked:
		return "BLOCKED"
	default:
		return "UNKNOWN"
	}
}

// blockedPatterns are system-destructive commands APCode refuses outright.
var blockedPatterns = []string{
	"mkfs",
	"dd if=",
	"shutdown",
	"poweroff",
	"reboot",
	"halt",
	"diskpart",
	"format c:",
	"format d:",
	"cipher /w",
	"reg add hklm",
	"> /dev/sda",
	"rm -rf /",
	"rd /s c:\\",
	":(){:|:&};:",
}

// safePrefixes are command prefixes considered read-only/validation safe.
// Matching is done on whole tokens so "go test" does not match "goto".
var safePrefixes = []string{
	"go test", "go build", "go vet", "go fmt", "go version", "gofmt", "go doc",
	"npm test", "npm run build", "npm run lint", "npm ci --dry-run",
	"pytest", "python -m compileall", "python --version", "python3 --version",
	"cargo test", "cargo build", "cargo check",
	"git status", "git diff", "git log", "git show", "git branch",
	"ls", "dir", "pwd", "which", "where", "echo",
	"node --version", "node -v", "npm --version",
	"go version",
}

// ClassifyCommand classifies a command (executable plus arguments) into its
// security class. The classification is intentionally conservative: anything
// not explicitly on the safe list requires approval, and anything matching a
// known system-destructive pattern is blocked entirely.
func ClassifyCommand(command string) CommandClass {
	cleaned := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(command))), " ")
	if cleaned == "" {
		return ClassApprovalRequired
	}
	for _, b := range blockedPatterns {
		if strings.Contains(cleaned, b) {
			return ClassBlocked
		}
	}
	// Bare Windows `format` (any target) is system-destructive.
	if cleaned == "format" || strings.HasPrefix(cleaned, "format ") {
		return ClassBlocked
	}
	for _, s := range safePrefixes {
		if cleaned == s || strings.HasPrefix(cleaned, s+" ") {
			return ClassSafe
		}
	}
	return ClassApprovalRequired
}

// hardenPathAgainstSymlinks verifies that abs (inside workspace after lexical
// cleaning) does not escape the workspace through symbolic links. For paths
// that do not yet exist it resolves the closest existing ancestor and checks
// that the remaining suffix stays under that ancestor's real location.
func hardenPathAgainstSymlinks(workspace, abs string) error {
	realWS, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		realWS = workspace
	}

	target := abs
	var suffix string
	if _, statErr := os.Lstat(abs); statErr != nil {
		// Path does not exist: resolve nearest existing ancestor.
		dir := filepath.Dir(abs)
		suffix = filepath.Base(abs)
		for {
			if _, err := os.Lstat(dir); err == nil {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			suffix = filepath.Join(filepath.Base(dir), suffix)
			dir = parent
		}
		target = dir
	}

	real, err := filepath.EvalSymlinks(target)
	if err != nil || real == "" {
		// Cannot resolve (race, permission, or Windows junction quirk);
		// fall back to lexical check.
		real = target
	}
	if suffix != "" {
		real = filepath.Join(real, suffix)
	}

	if realWS == "" {
		return nil
	}
	rel, err := filepath.Rel(realWS, real)
	if err != nil {
		return NewToolError(CodeOutsideWorkspace, "path escapes workspace via symlink", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || strings.HasPrefix(rel, "../") {
		return NewToolError(CodePathTraversal, "path escapes workspace via symlink", nil)
	}
	return nil
}
