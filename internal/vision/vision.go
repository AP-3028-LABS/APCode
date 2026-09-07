// Package vision handles local multimodal image processing for APCode.
// It validates image files, encodes them to base64, and provides helpers for
// multimodal payload generation for local runtimes (Ollama LLaVA, BakLLaVA, Qwen2-VL).
package vision

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Supported image extensions (lowercase).
var supportedExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
	".gif":  true,
	".bmp":  true,
}

// SupportedMime maps extension to MIME type.
var SupportedMime = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
}

// MaxImageSize is 20 MiB.
const MaxImageSize = 20 * 1024 * 1024

// Sentinel errors for callers using errors.Is.
var (
	ErrFileNotFound      = errors.New("vision: image file not found")
	ErrUnsupportedFormat = errors.New("vision: unsupported image format")
	ErrEmptyPath         = errors.New("vision: image path cannot be empty")
	ErrFileTooLarge      = errors.New("vision: image file too large")
	ErrNotVisionModel    = errors.New("vision: model does not support vision")
)

// Vision model identifiers (lowercase substrings that indicate vision capability).
var visionModelSubstrings = []string{
	"llava",
	"bakllava",
	"qwen2-vl",
	"qwen-vl",
	"vision",
	"moondream",
	"cogvlm",
	"minicpm",
	"internvl",
	"llava-phi",
	"phi-3-vision",
}

// IsSupportedExtension reports whether ext (with or without leading dot) is supported.
func IsSupportedExtension(ext string) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return false
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return supportedExts[ext]
}

// SupportedExtensions returns the list of supported extensions.
func SupportedExtensions() []string {
	return []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"}
}

// ValidateImageFile validates that path exists, is not a directory, has a supported
// extension, is not too large, and (optionally) has valid magic bytes for known formats.
func ValidateImageFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: path is empty", ErrEmptyPath)
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		return fmt.Errorf("vision: cannot stat image %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("vision: image path is a directory: %s", path)
	}
	if info.Size() > MaxImageSize {
		return fmt.Errorf("%w: %s is %d bytes (max %d)", ErrFileTooLarge, path, info.Size(), MaxImageSize)
	}
	if info.Size() == 0 {
		return fmt.Errorf("vision: image file is empty: %s", path)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !supportedExts[ext] {
		return fmt.Errorf("%w: %q (supported: PNG, JPEG, WEBP, GIF, BMP)", ErrUnsupportedFormat, ext)
	}
	// Magic byte check (best-effort, does not replace extension check).
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("vision: cannot open image %q: %w", path, err)
	}
	defer f.Close()
	header := make([]byte, 12)
	n, _ := f.Read(header)
	if n >= 8 {
		// PNG signature 89 50 4E 47 0D 0A 1A 0A
		if header[0] == 0x89 && header[1] == 0x50 && header[2] == 0x4E && header[3] == 0x47 {
			if ext != ".png" {
				// Allow but warn via error? For strict validation, require extension match.
				// We allow mismatched header vs extension as long as extension is supported;
				// header check is informative only.
			}
		} else if header[0] == 0xFF && header[1] == 0xD8 && header[2] == 0xFF {
			if ext != ".jpg" && ext != ".jpeg" {
			}
		} else if n >= 12 && header[0] == 'R' && header[1] == 'I' && header[2] == 'F' && header[3] == 'F' &&
			header[8] == 'W' && header[9] == 'E' && header[10] == 'B' && header[11] == 'P' {
			if ext != ".webp" {
			}
		} else if n >= 6 && header[0] == 'G' && header[1] == 'I' && header[2] == 'F' && header[3] == '8' {
			if ext != ".gif" {
			}
		} else if n >= 2 && header[0] == 'B' && header[1] == 'M' {
			if ext != ".bmp" {
			}
		}
	}
	return nil
}

// GetMimeType returns the MIME type for the image path based on extension.
func GetMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mime, ok := SupportedMime[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

// EncodeImageToBase64 validates and encodes the image file to a base64 string.
func EncodeImageToBase64(path string) (string, error) {
	if err := ValidateImageFile(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("vision: failed to read image %q: %w", path, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("vision: image file is empty: %s", path)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// IsVisionModel reports whether modelID indicates a multimodal vision model
// (LLaVA, BakLLaVA, Qwen2-VL, etc.) by substring match. Empty IDs are not vision.
func IsVisionModel(modelID string) bool {
	if strings.TrimSpace(modelID) == "" {
		return false
	}
	lower := strings.ToLower(modelID)
	for _, sub := range visionModelSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// ValidateVisionRequest validates that imagePath exists and that modelID is a vision model.
// It returns distinct errors for missing/unsupported images vs non-vision models
// so callers can render user-facing warnings.
func ValidateVisionRequest(imagePath, modelID string) error {
	if err := ValidateImageFile(imagePath); err != nil {
		return err
	}
	if modelID != "" && !IsVisionModel(modelID) {
		return fmt.Errorf("%w: %q is a text-only model and may not support image inputs (try llava, bakllava, or qwen2-vl)", ErrNotVisionModel, modelID)
	}
	return nil
}

// BuildOllamaPayload builds the JSON payload for Ollama's /api/generate with optional images.
// images are base64-encoded strings. If images is empty, the "images" key is omitted.
func BuildOllamaPayload(model, prompt string, images []string, stream bool, maxTokens int) map[string]any {
	payload := map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": stream,
	}
	if len(images) > 0 {
		payload["images"] = images
	}
	if maxTokens > 0 {
		payload["options"] = map[string]any{"num_predict": maxTokens}
	}
	return payload
}

// AttachmentChip returns the display string for an attached image, e.g. "[🖼 photo.png]".
func AttachmentChip(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	base := filepath.Base(path)
	if base == "." || base == "" {
		base = path
	}
	return "[🖼 " + base + "]"
}
