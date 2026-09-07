package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"apcode/internal/model"
	"apcode/internal/runtime"
	"apcode/internal/tui"
	"apcode/internal/vision"
)

// writeTestPNG writes a minimal 1x1 PNG so vision validation accepts it.
func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII="
	data, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

// writeTestImage writes a file with the given magic bytes plus padding, which
// satisfies vision.ValidateImageFile's best-effort magic-byte check for the
// matching extension (mirrors writeTestPNG for non-PNG formats).
func writeTestImage(t *testing.T, path string, magic []byte) {
	t.Helper()
	data := append(append([]byte{}, magic...), make([]byte, 64)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
}

// testVisionModel returns a valid, installed, vision-capable model metadata.
func testVisionModel() *model.ModelMetadata {
	return &model.ModelMetadata{
		ID:                   "llava-test",
		Name:                 "LLaVA Test",
		Provider:             "Test",
		Family:               "llava",
		ParameterCount:       1,
		Quantization:         model.QuantizationQ4,
		FileSizeBytes:        1_000_000,
		MinimumRAMBytes:      500_000,
		RecommendedRAMBytes:  1_000_000,
		ContextLength:        4096,
		Architecture:         model.ArchitectureLlama,
		Capabilities:         model.Capabilities{model.CapabilityCodeGeneration},
		RuntimeCompatibility: []model.Runtime{model.RuntimeLlamaCPP},
		Installed:            true,
		InstallPath:          "/tmp/llava-test.gguf",
	}
}

func TestImageCommandInterceptedNoLLMInvoke(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("/image\n\x1b\n"), out, errOut)
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	calls := 0
	mock := runtime.NewMockRuntime(runtime.MockConfig{})
	mock.GenerateFunc = func(ctx context.Context, req runtime.GenerateRequest) (*runtime.GenerateResponse, error) {
		calls++
		return &runtime.GenerateResponse{Text: "should never happen", TokensGenerated: 1, FinishReason: "stop"}, nil
	}
	repl.Runtime = mock
	repl.Model = testVisionModel()

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	o := out.String()
	if calls != 0 {
		t.Fatalf("/image must be intercepted as a TUI command and never invoke the LLM; Generate called %d times", calls)
	}
	if !strings.Contains(o, "Image attachment picker") {
		t.Error("expected the file picker to render")
	}
	if strings.Contains(o, "Thinking") {
		t.Error("/image must not start agent inference (no Thinking spinner)")
	}
	if !strings.Contains(o, "cancelled") {
		t.Error("expected cancellation message after Esc")
	}
}

func TestImagePickerValidSelection(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	img := filepath.Join(tmp, "screenshot.png")
	writeTestPNG(t, img)
	os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte("hi"), 0o644)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("1\n"), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.Workspace = tmp

	repl.handleImageAttach("/image")

	if len(repl.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(repl.Attachments))
	}
	a := repl.Attachments[0]
	if a.Path != img || a.Filename != "screenshot.png" {
		t.Errorf("wrong attachment: %+v", a)
	}
	if a.Mime != "image/png" {
		t.Errorf("wrong mime: %q", a.Mime)
	}
	if a.Size == 0 {
		t.Error("size should be set")
	}
	o := out.String()
	if !strings.Contains(o, "[🖼 screenshot.png]") {
		t.Errorf("expected chip in output, got:\n%s", o)
	}
	if !strings.Contains(o, "Attached") {
		t.Errorf("expected attach confirmation, got:\n%s", o)
	}
}

func TestImagePickerUnsupportedExtension(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "doc.txt"), []byte("hello"), 0o644)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("doc.txt\n\x1b\n"), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.Workspace = tmp

	repl.handleImageAttach("/image")

	if len(repl.Attachments) != 0 {
		t.Fatalf("unsupported file must not be attached, got %d", len(repl.Attachments))
	}
	o := out.String()
	if !strings.Contains(o, "unsupported") {
		t.Errorf("expected unsupported-format error, got:\n%s", o)
	}
}

func TestImagePickerMissingFile(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("missing.png\n\x1b\n"), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.Workspace = tmp

	repl.handleImageAttach("/image")

	if len(repl.Attachments) != 0 {
		t.Fatalf("missing file must not be attached, got %d", len(repl.Attachments))
	}
	if !strings.Contains(out.String(), "File not found") {
		t.Errorf("expected file-not-found error, got:\n%s", out.String())
	}
}

func TestImagePickerCancel(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("\x1b\n"), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.Workspace = tmp

	repl.handleImageAttach("/image")

	if len(repl.Attachments) != 0 {
		t.Fatalf("cancel must not attach anything, got %d", len(repl.Attachments))
	}
	if !strings.Contains(out.String(), "cancelled") {
		t.Errorf("expected cancel message, got:\n%s", out.String())
	}
}

func TestImageAttachMultipleAndByPath(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.png")
	b := filepath.Join(tmp, "b.png")
	writeTestPNG(t, a)
	writeTestPNG(t, b)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.Workspace = tmp

	// REPL /image <path> form must keep working and be additive.
	repl.handleImageAttach("/image " + a)
	repl.handleImageAttach("/attach " + b)

	if len(repl.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(repl.Attachments))
	}
	chips := tui.AttachmentChips(repl.attachedPaths())
	if !strings.Contains(chips, "a.png") || !strings.Contains(chips, "b.png") {
		t.Errorf("expected both chips, got %q", chips)
	}
	if !strings.Contains(out.String(), "[🖼 a.png] [🖼 b.png]") {
		t.Errorf("expected both chips in output, got:\n%s", out.String())
	}

	// Sending a prompt must clear all attachments.
	repl.clearAttachments()
	if len(repl.Attachments) != 0 {
		t.Error("clearAttachments should reset the composer")
	}
}

func TestImageCommandPaletteListsImage(t *testing.T) {
	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.printHelp()
	o := out.String()
	if !strings.Contains(o, "/image") {
		t.Errorf("command palette must list /image, got:\n%s", o)
	}
	found := false
	for _, c := range tui.DefaultMenuCommands() {
		if c.Name == "/image" {
			found = true
			break
		}
	}
	if !found {
		t.Error("DefaultMenuCommands must include /image")
	}
}

func TestAttachmentsIncludedInMultimodalRequest(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.png")
	b := filepath.Join(tmp, "b.png")
	writeTestPNG(t, a)
	writeTestPNG(t, b)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.Workspace = tmp
	repl.Model = testVisionModel()

	var got []string
	mock := runtime.NewMockRuntime(runtime.MockConfig{})
	mock.GenerateFunc = func(ctx context.Context, req runtime.GenerateRequest) (*runtime.GenerateResponse, error) {
		got = append([]string(nil), req.Images...)
		return &runtime.GenerateResponse{Text: "A diagram with two nodes.", TokensGenerated: 7, FinishReason: "stop"}, nil
	}
	repl.Runtime = mock

	st, serr := os.Stat(a)
	if serr != nil {
		t.Fatalf("stat %s: %v", a, serr)
	}
	repl.Attachments = []ImageAttachment{
		{Path: a, Filename: filepath.Base(a), Mime: vision.GetMimeType(a), Size: st.Size()},
		{Path: b, Filename: filepath.Base(b), Mime: vision.GetMimeType(b), Size: st.Size()},
	}

	resp, err := repl.runAgent(context.Background(), "describe the diagram")
	if err != nil {
		t.Fatalf("runAgent failed: %v", err)
	}
	if !strings.Contains(resp, "diagram") {
		t.Errorf("unexpected response: %q", resp)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 images in multimodal request, got %d", len(got))
	}
	for i, img := range got {
		data, derr := base64.StdEncoding.DecodeString(img)
		if derr != nil {
			t.Fatalf("image %d not valid base64: %v", i, derr)
		}
		if len(data) < 8 || data[0] != 0x89 || data[1] != 0x50 {
			t.Errorf("image %d does not decode to a PNG", i)
		}
	}
}

// TestImagePathAttachmentFormats verifies every supported format attaches via
// the /image <path> form with the correct MIME (PNG, JPEG, WEBP, GIF, BMP).
func TestImagePathAttachmentFormats(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	cases := []struct {
		ext   string
		mime  string
		magic []byte
	}{
		{".png", "image/png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}},
		{".jpg", "image/jpeg", []byte{0xff, 0xd8, 0xff}},
		{".jpeg", "image/jpeg", []byte{0xff, 0xd8, 0xff}},
		{".webp", "image/webp", []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}},
		{".gif", "image/gif", []byte{'G', 'I', 'F', '8', '9', 'a'}},
		{".bmp", "image/bmp", []byte{'B', 'M'}},
	}
	for _, tc := range cases {
		t.Run(strings.TrimPrefix(tc.ext, "."), func(t *testing.T) {
			tmp := t.TempDir()
			img := filepath.Join(tmp, "img"+tc.ext)
			writeTestImage(t, img, tc.magic)

			out := &bytes.Buffer{}
			repl, err := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
			if err != nil {
				t.Fatalf("NewREPL failed: %v", err)
			}
			repl.handleImageAttach("/image " + img)

			if len(repl.Attachments) != 1 {
				t.Fatalf("expected 1 attachment, got %d", len(repl.Attachments))
			}
			a := repl.Attachments[0]
			if a.Mime != tc.mime {
				t.Errorf("expected mime %q, got %q", tc.mime, a.Mime)
			}
			if a.Filename != "img"+tc.ext {
				t.Errorf("expected filename %q, got %q", "img"+tc.ext, a.Filename)
			}
			// The validation layer must remain the final gate: a file with
			// supported magic bytes must flow into the multimodal pipeline.
			if err := vision.ValidateImageFile(a.Path); err != nil {
				t.Errorf("attached image rejected by validation: %v", err)
			}
			b64, err := vision.EncodeImageToBase64(a.Path)
			if err != nil {
				t.Fatalf("attached image must encode to base64: %v", err)
			}
			if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
				t.Fatalf("attached image payload not valid base64: %v", err)
			}
		})
	}
}

// TestImagePathAttachmentUnsupported verifies the /image <path> form rejects
// files with a renamed/unrecognized extension (final validation layer).
func TestImagePathAttachmentUnsupported(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	doc := filepath.Join(tmp, "doc.txt")
	os.WriteFile(doc, []byte("hello"), 0o644)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.handleImageAttach("/image " + doc)

	if len(repl.Attachments) != 0 {
		t.Fatalf("unsupported file must not be attached, got %d", len(repl.Attachments))
	}
	if !strings.Contains(out.String(), "unsupported") {
		t.Errorf("expected unsupported-format error, got:\n%s", out.String())
	}
}

// TestImagePathAttachmentMissing verifies the /image <path> form handles a
// nonexistent file without crashing and without attaching anything.
func TestImagePathAttachmentMissing(t *testing.T) {
	repl, err := NewREPL(bytes.NewBufferString(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.handleImageAttach("/image " + filepath.Join(t.TempDir(), "missing.png"))
	if len(repl.Attachments) != 0 {
		t.Fatalf("missing file must not be attached, got %d", len(repl.Attachments))
	}
}

// TestImagePickerMultipleAttachments verifies that repeated /image operations
// through the picker accumulate multiple attachments (no accidental reset).
func TestImagePickerMultipleAttachments(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.png")
	b := filepath.Join(tmp, "b.png")
	writeTestPNG(t, a)
	writeTestPNG(t, b)

	out := &bytes.Buffer{}
	repl, err := NewREPL(bytes.NewBufferString("1\n2\n"), out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewREPL failed: %v", err)
	}
	repl.Workspace = tmp

	repl.handleImageAttach("/image")
	repl.handleImageAttach("/image")

	if len(repl.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(repl.Attachments))
	}
	chips := tui.AttachmentChips(repl.attachedPaths())
	if !strings.Contains(chips, "a.png") || !strings.Contains(chips, "b.png") {
		t.Errorf("expected both chips, got %q", chips)
	}
}
