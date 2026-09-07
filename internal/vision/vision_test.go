package vision

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(path string) error {
	// Minimal 1x1 PNG (67 bytes) base64 decoded
	const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII="
	data, _ := base64.StdEncoding.DecodeString(pngB64)
	return os.WriteFile(path, data, 0o644)
}

func writeJPEG(path string) error {
	// Minimal JPEG header FF D8 FF E0 + dummy
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}
	// pad to make valid size
	for i := 0; i < 100; i++ {
		data = append(data, byte(i))
	}
	return os.WriteFile(path, data, 0o644)
}

func writeWebP(path string) error {
	// RIFF....WEBPVP8 header + dummy payload
	data := []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
	for i := 0; i < 64; i++ {
		data = append(data, byte(i))
	}
	return os.WriteFile(path, data, 0o644)
}

func writeGIF(path string) error {
	return os.WriteFile(path, []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\x00\x00\x00\x00\x3b"), 0o644)
}

func writeBMP(path string) error {
	// BMP header "BM" + dummy pixel data
	data := []byte{'B', 'M', 26, 0, 0, 0, 0, 0, 0, 0, 26, 0, 0, 0}
	for i := 0; i < 8; i++ {
		data = append(data, byte(i))
	}
	return os.WriteFile(path, data, 0o644)
}

func TestValidateImageFileMissing(t *testing.T) {
	err := ValidateImageFile("/nonexistent/path.png")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestValidateImageFileEmptyPath(t *testing.T) {
	if err := ValidateImageFile(""); err == nil {
		t.Error("empty path should error")
	}
	if err := ValidateImageFile("   "); err == nil {
		t.Error("whitespace path should error")
	}
}

func TestValidateImageFileUnsupportedFormat(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "file.txt")
	os.WriteFile(p, []byte("hello"), 0o644)
	err := ValidateImageFile(p)
	if err == nil {
		t.Fatal("expected unsupported format error")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("wrong error: %v", err)
	}
	p2 := filepath.Join(tmp, "file.exe")
	os.WriteFile(p2, []byte("MZ\x90\x00"), 0o644)
	if err := ValidateImageFile(p2); err == nil {
		t.Error("exe should be unsupported")
	}
}

func TestValidateImageFileDirectory(t *testing.T) {
	tmp := t.TempDir()
	if err := ValidateImageFile(tmp); err == nil {
		t.Error("directory should error")
	}
}

func TestValidateImageFilePNGAndJPEGValid(t *testing.T) {
	tmp := t.TempDir()
	png := filepath.Join(tmp, "test.png")
	if err := writePNG(png); err != nil {
		t.Fatalf("write png: %v", err)
	}
	if err := ValidateImageFile(png); err != nil {
		t.Errorf("png validate failed: %v", err)
	}
	jpg := filepath.Join(tmp, "photo.jpg")
	if err := writeJPEG(jpg); err != nil {
		t.Fatalf("write jpg: %v", err)
	}
	if err := ValidateImageFile(jpg); err != nil {
		t.Errorf("jpg validate failed: %v", err)
	}
	jpeg := filepath.Join(tmp, "photo.jpeg")
	if err := writeJPEG(jpeg); err != nil {
		t.Fatalf("write jpeg: %v", err)
	}
	if err := ValidateImageFile(jpeg); err != nil {
		t.Errorf("jpeg validate failed: %v", err)
	}
}

func TestValidateImageFileExtendedFormats(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		name  string
		ext   string
		write func(string) error
	}{
		{"webp", ".webp", writeWebP},
		{"gif", ".gif", writeGIF},
		{"bmp", ".bmp", writeBMP},
	}
	for _, tc := range cases {
		p := filepath.Join(tmp, "test"+tc.ext)
		if err := tc.write(p); err != nil {
			t.Fatalf("write %s: %v", tc.name, err)
		}
		if err := ValidateImageFile(p); err != nil {
			t.Errorf("%s validate failed: %v", tc.name, err)
		}
		if m := GetMimeType(p); m == "application/octet-stream" {
			t.Errorf("%s mime not set", tc.name)
		}
	}
}

func TestEncodeImageToBase64(t *testing.T) {
	tmp := t.TempDir()
	png := filepath.Join(tmp, "a.png")
	if err := writePNG(png); err != nil {
		t.Fatalf("write: %v", err)
	}
	b64, err := EncodeImageToBase64(png)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if b64 == "" {
		t.Error("empty base64")
	}
	// Verify it decodes to original PNG magic
	data, _ := base64.StdEncoding.DecodeString(b64)
	if len(data) < 8 || data[0] != 0x89 || data[1] != 0x50 {
		t.Error("decoded data not PNG")
	}
	// Missing file
	if _, err := EncodeImageToBase64(filepath.Join(tmp, "missing.png")); err == nil {
		t.Error("expected error for missing")
	}
	// Unsupported
	txt := filepath.Join(tmp, "x.txt")
	os.WriteFile(txt, []byte("hi"), 0o644)
	if _, err := EncodeImageToBase64(txt); err == nil {
		t.Error("expected error for unsupported")
	}
}

func TestIsVisionModel(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"llava", true},
		{"llava:7b", true},
		{"bakllava", true},
		{"qwen2-vl", true},
		{"Qwen2-VL-7B", true},
		{"moondream", true},
		{"cogvlm", true},
		{"gemma-2b-q4", false},
		{"phi-3-mini-q4", false},
		{"", false},
		{"codellama-7b-q4", false},
	}
	for _, tc := range cases {
		if got := IsVisionModel(tc.id); got != tc.want {
			t.Errorf("IsVisionModel(%q)=%v want %v", tc.id, got, tc.want)
		}
	}
}

func TestValidateVisionRequest(t *testing.T) {
	tmp := t.TempDir()
	png := filepath.Join(tmp, "ok.png")
	writePNG(png)
	// Valid vision
	if err := ValidateVisionRequest(png, "llava"); err != nil {
		t.Errorf("valid vision request failed: %v", err)
	}
	// Missing file
	if err := ValidateVisionRequest(filepath.Join(tmp, "missing.png"), "llava"); err == nil {
		t.Error("missing should error")
	}
	// Unsupported format
	txt := filepath.Join(tmp, "a.txt")
	os.WriteFile(txt, []byte("x"), 0o644)
	if err := ValidateVisionRequest(txt, "llava"); err == nil {
		t.Error("unsupported should error")
	}
	// Non-vision model should warn
	if err := ValidateVisionRequest(png, "gemma-2b-q4"); err == nil {
		t.Error("non-vision model should error")
	} else if !strings.Contains(err.Error(), "text-only") {
		t.Errorf("wrong error for non-vision: %v", err)
	}
	// Empty modelID should pass (unknown model, skip check)
	if err := ValidateVisionRequest(png, ""); err != nil {
		t.Errorf("empty model should pass: %v", err)
	}
}

func TestBuildOllamaPayload(t *testing.T) {
	payload := BuildOllamaPayload("llava", "describe", []string{"base64data"}, false, 100)
	if payload["model"] != "llava" || payload["prompt"] != "describe" {
		t.Errorf("payload wrong: %v", payload)
	}
	if imgs, ok := payload["images"].([]string); !ok || len(imgs) != 1 || imgs[0] != "base64data" {
		t.Errorf("images missing: %v", payload)
	}
	if payload["stream"] != false {
		t.Error("stream wrong")
	}
	// No images should omit key
	p2 := BuildOllamaPayload("llava", "hi", nil, true, 0)
	if _, ok := p2["images"]; ok {
		t.Error("images should be omitted when empty")
	}
	if _, ok := p2["options"]; ok {
		t.Error("options should be omitted when maxTokens 0")
	}
}

func TestAttachmentChip(t *testing.T) {
	if got := AttachmentChip(""); got != "" {
		t.Errorf("empty path should be empty, got %q", got)
	}
	if got := AttachmentChip("/tmp/photo.png"); got != "[🖼 photo.png]" {
		t.Errorf("chip wrong: %q", got)
	}
	if got := AttachmentChip("a.jpg"); !strings.Contains(got, "a.jpg") {
		t.Errorf("chip missing filename: %q", got)
	}
}

func TestIsSupportedExtension(t *testing.T) {
	valid := []string{".png", "png", ".JPEG", ".webp", "gif", ".bmp"}
	for _, ext := range valid {
		if !IsSupportedExtension(ext) {
			t.Errorf("supported ext should be true: %q", ext)
		}
	}
	if IsSupportedExtension(".txt") || IsSupportedExtension(".exe") || IsSupportedExtension("") {
		t.Error("unsupported ext should be false")
	}
}

func TestGetMimeType(t *testing.T) {
	if GetMimeType("a.png") != "image/png" {
		t.Error("png mime")
	}
	if GetMimeType("b.jpg") != "image/jpeg" || GetMimeType("c.jpeg") != "image/jpeg" {
		t.Error("jpeg mime")
	}
	if GetMimeType("d.webp") != "image/webp" || GetMimeType("e.gif") != "image/gif" || GetMimeType("f.bmp") != "image/bmp" {
		t.Error("extended mime")
	}
}
