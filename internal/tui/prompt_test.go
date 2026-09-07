package tui

import "testing"

func TestAttachmentChips(t *testing.T) {
	SetColorsEnabled(false)
	defer SetColorsEnabled(true)

	if got := AttachmentChips(nil); got != "" {
		t.Errorf("nil should be empty, got %q", got)
	}
	if got := AttachmentChips([]string{"", "  "}); got != "" {
		t.Errorf("empty paths should be empty, got %q", got)
	}
	got := AttachmentChips([]string{"/tmp/a.png", "b.jpg"})
	want := "[🖼 a.png] [🖼 b.jpg]"
	if got != want {
		t.Errorf("chips wrong: %q want %q", got, want)
	}
}

func TestInputStatusLinesWithImages(t *testing.T) {
	SetColorsEnabled(false)
	defer SetColorsEnabled(true)

	line := InputStatusLinesWithImages("native", "llava", "ollama", "", []string{"/tmp/a.png", "/tmp/b.png"})
	for _, want := range []string{"[🖼 a.png]", "[🖼 b.png]", "native", "llava"} {
		if !containsPlain(line, want) {
			t.Errorf("status line missing %q: %q", want, line)
		}
	}
	// Single-field compatibility path.
	single := InputStatusLineWithImage("native", "llava", "ollama", "", "/tmp/a.png")
	if !containsPlain(single, "[🖼 a.png]") {
		t.Errorf("single-image status missing chip: %q", single)
	}
	// No images -> same as plain InputStatusLine.
	plain := InputStatusLine("native", "llava", "ollama", "")
	if got := InputStatusLinesWithImages("native", "llava", "ollama", "", nil); got != plain {
		t.Errorf("nil-images should equal plain: %q vs %q", got, plain)
	}
}

func containsPlain(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
