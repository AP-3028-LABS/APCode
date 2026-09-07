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

// helpers for vision workflow tests

func writeVisionPNG(t *testing.T, path string) {
	t.Helper()
	const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII="
	data, _ := base64.StdEncoding.DecodeString(pngB64)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

func textOnlyModel() *model.ModelMetadata {
	return &model.ModelMetadata{
		ID:                   "qwen2.5-coder-7b-q4",
		Name:                 "Qwen2.5-Coder 7B Q4",
		Provider:             "Qwen",
		Family:               "Qwen2.5-Coder",
		ParameterCount:       7,
		Quantization:         model.QuantizationQ4,
		FileSizeBytes:        4_200_000_000,
		MinimumRAMBytes:      6_000_000_000,
		RecommendedRAMBytes:  8_000_000_000,
		ContextLength:        32768,
		Architecture:         model.ArchitectureQwen,
		Capabilities:         model.Capabilities{model.CapabilityCodeGeneration},
		Vision:               false,
		RuntimeCompatibility: []model.Runtime{model.RuntimeLlamaCPP, model.RuntimeOllama},
		Installed:            true,
		InstallPath:          "/tmp/qwen2.5-coder-7b-q4.gguf",
	}
}

func visionModelExplicit() *model.ModelMetadata {
	return &model.ModelMetadata{
		ID:                   "qwen2-vl-7b-q4",
		Name:                 "Qwen2-VL 7B Q4",
		Provider:             "Qwen",
		Family:               "Qwen2-VL",
		ParameterCount:       7,
		Quantization:         model.QuantizationQ4,
		FileSizeBytes:        4_500_000_000,
		MinimumRAMBytes:      6_000_000_000,
		RecommendedRAMBytes:  8_000_000_000,
		ContextLength:        32768,
		Architecture:         model.ArchitectureQwen,
		Capabilities:         model.Capabilities{model.CapabilityCodeGeneration, model.CapabilityVision},
		Vision:               true,
		RuntimeCompatibility: []model.Runtime{model.RuntimeLlamaCPP, model.RuntimeOllama},
		Installed:            true,
		InstallPath:          "/tmp/qwen2-vl-7b-q4.gguf",
	}
}

func setupInstalledVisionDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Create a fake vision model file that matches BuiltInCatalog ID
	// Use moondream-1.8b-q4 as smallest to avoid RAM issues
	for _, id := range []string{"qwen2-vl-7b-q4", "llava-7b-q4"} {
		path := filepath.Join(dir, id+".gguf")
		if err := os.WriteFile(path, []byte("fake model data for "+id), 0o644); err != nil {
			t.Fatalf("write fake model: %v", err)
		}
	}
	return dir
}

// TestVisionModelAcceptsImage verifies case A: vision model supports image input
func TestVisionModelAcceptsImage(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)

	out := &bytes.Buffer{}
	repl, _ := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	repl.Model = visionModelExplicit()
	repl.ModelDir = t.TempDir() // empty but vision model is active, no need to discover

	if err := repl.handleVisionAwareAttach(img); err != nil {
		t.Fatalf("handleVisionAwareAttach failed: %v", err)
	}
	if len(repl.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(repl.Attachments))
	}
	o := out.String()
	if !strings.Contains(o, "Active model supports image input") {
		t.Errorf("expected vision success message, got:\n%s", o)
	}
	if !strings.Contains(o, "[🖼") {
		t.Errorf("expected chip, got:\n%s", o)
	}
	// Ensure isVisionCapable true
	if !isVisionCapableModel(repl.Model) {
		t.Error("vision model should be capable")
	}
}

// TestTextOnlyModelRejectsImageRequest verifies that a text-only model blocks multimodal requests
func TestTextOnlyModelRejectsImageRequest(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)

	out := &bytes.Buffer{}
	repl, _ := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	repl.Model = textOnlyModel()
	repl.ModelDir = t.TempDir() // empty -> no vision installed

	// Attach via vision workflow (no switch available, will preserve but warn)
	if err := repl.handleVisionAwareAttach(img); err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	if len(repl.Attachments) != 1 {
		t.Fatalf("image should be preserved even when no vision model, got %d", len(repl.Attachments))
	}
	o := out.String()
	if !strings.Contains(o, "No vision-capable") {
		t.Errorf("expected no-vision message, got:\n%s", o)
	}
	if !strings.Contains(o, "apcode models install") {
		t.Errorf("expected install command, got:\n%s", o)
	}

	// Now try to send via runAgent: should be blocked and not reach runtime
	mock := runtime.NewMockRuntime(runtime.MockConfig{})
	calls := 0
	mock.GenerateFunc = func(ctx context.Context, req runtime.GenerateRequest) (*runtime.GenerateResponse, error) {
		calls++
		if len(req.Images) > 0 {
			t.Errorf("text-only model should never receive images, got %d", len(req.Images))
		}
		return &runtime.GenerateResponse{Text: "ok", TokensGenerated: 1, FinishReason: "stop"}, nil
	}
	repl.Runtime = mock

	out.Reset()
	_, err := repl.runAgent(context.Background(), "describe image")
	if err == nil {
		t.Fatal("expected vision block error")
	}
	if !strings.Contains(err.Error(), "vision") && !strings.Contains(err.Error(), "No vision") {
		t.Errorf("expected vision error, got %v", err)
	}
	if calls != 0 {
		t.Errorf("Generate should not be called for blocked vision request, got %d", calls)
	}
	// Attachments should be preserved after block (not cleared)
	if len(repl.Attachments) != 1 {
		t.Errorf("attachments should be preserved after blocked send, got %d", len(repl.Attachments))
	}
}

// TestTextOnlyPlusInstalledVisionOffersSwitch verifies case B: switch prompt
func TestTextOnlyPlusInstalledVisionOffersSwitch(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)

	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)
	visionDir := setupInstalledVisionDir(t)

	// Input buffer contains selection "1\n" for vision model picker
	in := bytes.NewBufferString("1\n")
	out := &bytes.Buffer{}
	repl, _ := NewREPL(in, out, &bytes.Buffer{})
	repl.Workspace = tmp
	repl.Model = textOnlyModel()
	repl.ModelDir = visionDir
	repl.Runtime = runtime.NewMockRuntime(runtime.MockConfig{})
	// Ensure runtime compatible (mock accepts all)

	if err := repl.handleVisionAwareAttach(img); err != nil {
		t.Fatalf("handleVisionAwareAttach failed: %v", err)
	}
	// Should have switched to vision model
	if repl.Model == nil {
		t.Fatal("model should not be nil after switch")
	}
	if !isVisionCapableModel(repl.Model) {
		t.Errorf("expected switched model to be vision capable, got %q", repl.Model.ID)
	}
	if len(repl.Attachments) != 1 {
		t.Fatalf("expected 1 attachment after switch, got %d", len(repl.Attachments))
	}
	o := out.String()
	if !strings.Contains(o, "Switching model") {
		t.Errorf("expected switching banner, got:\n%s", o)
	}
	if !strings.Contains(o, "Vision model selected") {
		t.Errorf("expected vision selected, got:\n%s", o)
	}
	if !strings.Contains(o, "[🖼") {
		t.Errorf("expected chip after switch, got:\n%s", o)
	}
	// Now verify that runAgent will succeed with images (vision model)
	mock := repl.Runtime.(*runtime.MockRuntime)
	calls := 0
	var gotImages []string
	mock.GenerateFunc = func(ctx context.Context, req runtime.GenerateRequest) (*runtime.GenerateResponse, error) {
		calls++
		gotImages = append([]string(nil), req.Images...)
		return &runtime.GenerateResponse{Text: "vision ok", TokensGenerated: 1, FinishReason: "stop"}, nil
	}

	resp, err := repl.runAgent(context.Background(), "describe")
	if err != nil {
		t.Fatalf("runAgent with vision model should succeed: %v", err)
	}
	if !strings.Contains(resp, "vision ok") {
		t.Errorf("unexpected resp %q", resp)
	}
	if calls != 1 {
		t.Fatalf("expected Generate called once, got %d", calls)
	}
	if len(gotImages) != 1 {
		t.Errorf("expected 1 image in request, got %d", len(gotImages))
	}
}

// TestTextOnlyNoVisionGivesActionableMessage verifies case C: no vision installed message
func TestTextOnlyNoVisionGivesActionableMessage(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)
	out := &bytes.Buffer{}
	repl, _ := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	repl.Model = textOnlyModel()
	repl.ModelDir = t.TempDir() // empty
	repl.Runtime = runtime.NewMockRuntime(runtime.MockConfig{})

	if err := repl.handleVisionAwareAttach(img); err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	o := out.String()
	if !strings.Contains(o, "No vision-capable") {
		t.Errorf("expected no-vision message, got:\n%s", o)
	}
	if !strings.Contains(o, "apcode models install") {
		t.Errorf("expected install command, got:\n%s", o)
	}
	if !strings.Contains(o, "Recommended models") {
		t.Errorf("expected recommended, got:\n%s", o)
	}
	// Image preserved
	if len(repl.Attachments) != 1 {
		t.Error("image should be preserved")
	}
}

// TestImagePreservedWhileSelection verifies cancellation preserves image
func TestImagePreservedWhileSelection(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)
	visionDir := setupInstalledVisionDir(t)
	// Cancel input: Esc
	in := bytes.NewBufferString("\x1b\n")
	out := &bytes.Buffer{}
	repl, _ := NewREPL(in, out, &bytes.Buffer{})
	repl.Model = textOnlyModel()
	repl.ModelDir = visionDir
	repl.Runtime = runtime.NewMockRuntime(runtime.MockConfig{})

	if err := repl.handleVisionAwareAttach(img); err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	// Model should NOT have switched (still text)
	if repl.Model.ID != textOnlyModel().ID {
		t.Errorf("model should remain text-only after cancel, got %q", repl.Model.ID)
	}
	// But image preserved
	if len(repl.Attachments) != 1 {
		t.Fatalf("image should be preserved after cancel, got %d", len(repl.Attachments))
	}
	o := out.String()
	if !strings.Contains(o, "cancelled") {
		t.Errorf("expected cancelled message, got:\n%s", o)
	}
	// Simulate empty input cancel as well
	in2 := bytes.NewBufferString("\n")
	out.Reset()
	repl2, _ := NewREPL(in2, out, &bytes.Buffer{})
	repl2.Model = textOnlyModel()
	repl2.ModelDir = visionDir
	repl2.Runtime = runtime.NewMockRuntime(runtime.MockConfig{})
	img2 := filepath.Join(tmp, "b.png")
	writeVisionPNG(t, img2)
	if err := repl2.handleVisionAwareAttach(img2); err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	if len(repl2.Attachments) != 1 {
		t.Error("image should be preserved on empty cancel")
	}
}

// TestMultipleImagesPreserved verifies multiple images handling
func TestMultipleImagesPreserved(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.png")
	b := filepath.Join(tmp, "b.png")
	writeVisionPNG(t, a)
	writeVisionPNG(t, b)

	out := &bytes.Buffer{}
	repl, _ := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	repl.Model = visionModelExplicit()
	repl.ModelDir = t.TempDir()

	if err := repl.handleVisionAwareAttach(a); err != nil {
		t.Fatalf("first attach failed: %v", err)
	}
	if err := repl.handleVisionAwareAttach(b); err != nil {
		t.Fatalf("second attach failed: %v", err)
	}
	if len(repl.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(repl.Attachments))
	}
	// Ensure both sent to vision runtime
	mock := runtime.NewMockRuntime(runtime.MockConfig{})
	var got int
	mock.GenerateFunc = func(ctx context.Context, req runtime.GenerateRequest) (*runtime.GenerateResponse, error) {
		got = len(req.Images)
		return &runtime.GenerateResponse{Text: "ok", TokensGenerated: 1, FinishReason: "stop"}, nil
	}
	repl.Runtime = mock
	_, err := repl.runAgent(context.Background(), "describe both")
	if err != nil {
		t.Fatalf("runAgent failed: %v", err)
	}
	if got != 2 {
		t.Errorf("expected 2 images sent, got %d", got)
	}
}

// TestNoInvalidRequestReachesTextOnly verifies blocking prevents invalid request
func TestNoInvalidRequestReachesTextOnly(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)

	repl, _ := NewREPL(bytes.NewBufferString(""), &bytes.Buffer{}, &bytes.Buffer{})
	repl.Model = textOnlyModel()
	repl.ModelDir = t.TempDir()

	// Directly append attachment to simulate already attached (bypass attach workflow)
	repl.Attachments = []ImageAttachment{{Path: img, Filename: "a.png", Mime: vision.GetMimeType(img), Size: 100}}

	mock := runtime.NewMockRuntime(runtime.MockConfig{})
	calls := 0
	mock.GenerateFunc = func(ctx context.Context, req runtime.GenerateRequest) (*runtime.GenerateResponse, error) {
		calls++
		if len(req.Images) != 0 {
			t.Errorf("should not receive images for text-only, got %d", len(req.Images))
		}
		return &runtime.GenerateResponse{Text: "should not happen", TokensGenerated: 1, FinishReason: "stop"}, nil
	}
	repl.Runtime = mock

	_, err := repl.runAgent(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error for text-only with images")
	}
	if calls != 0 {
		t.Error("Generate must not be called for blocked request")
	}
}

// TestExistingImagePathBehavior verifies /image <path> still works
func TestExistingImagePathBehavior(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)

	out := &bytes.Buffer{}
	repl, _ := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	repl.Workspace = tmp
	repl.Model = visionModelExplicit()
	repl.ModelDir = t.TempDir()

	repl.handleImageAttach("/image " + img)
	if len(repl.Attachments) != 1 {
		t.Fatalf("expected 1 attachment via /image <path>, got %d", len(repl.Attachments))
	}
	repl.handleImageAttach("/attach " + img)
	if len(repl.Attachments) != 2 {
		t.Fatalf("expected 2 via /attach, got %d", len(repl.Attachments))
	}
	if !strings.Contains(out.String(), "[🖼") {
		t.Error("expected chip")
	}
}

// TestClearBehavior verifies /clear clears attachments
func TestClearBehavior(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)

	repl, _ := NewREPL(bytes.NewBufferString(""), &bytes.Buffer{}, &bytes.Buffer{})
	repl.Model = visionModelExplicit()
	repl.handleVisionAwareAttach(img)
	if len(repl.Attachments) != 1 {
		t.Fatal("should have 1")
	}
	repl.clearAttachments()
	if len(repl.Attachments) != 0 {
		t.Error("clear should reset")
	}
	// Also via slash command
	repl.handleVisionAwareAttach(img)
	repl.handleSlashCommand(context.Background(), "/clear")
	if len(repl.Attachments) != 0 {
		t.Error("/clear should clear attachments")
	}
}

// TestTextOnlyCodingWorkflowUnchanged verifies text-only without images still works
func TestTextOnlyCodingWorkflowUnchanged(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	repl, _ := NewREPL(bytes.NewBufferString(""), &bytes.Buffer{}, &bytes.Buffer{})
	repl.Model = textOnlyModel()
	mock := runtime.NewMockRuntime(runtime.MockConfig{})
	repl.Runtime = mock
	mock.GenerateFunc = func(ctx context.Context, req runtime.GenerateRequest) (*runtime.GenerateResponse, error) {
		if len(req.Images) != 0 {
			t.Error("text-only without images should have no images")
		}
		return &runtime.GenerateResponse{Text: "coding answer", TokensGenerated: 1, FinishReason: "stop"}, nil
	}
	resp, err := repl.runAgent(context.Background(), "write function")
	if err != nil {
		t.Fatalf("runAgent failed: %v", err)
	}
	if !strings.Contains(resp, "coding answer") {
		t.Errorf("unexpected resp %q", resp)
	}
}

// TestVisionCapabilityUnknown verifies unknown handling (nil model)
func TestVisionCapabilityUnknown(t *testing.T) {
	tui.SetColorsEnabled(false)
	defer tui.SetColorsEnabled(true)
	tmp := t.TempDir()
	img := filepath.Join(tmp, "a.png")
	writeVisionPNG(t, img)
	out := &bytes.Buffer{}
	repl, _ := NewREPL(bytes.NewBufferString(""), out, &bytes.Buffer{})
	repl.Model = nil
	repl.ModelDir = t.TempDir()
	if err := repl.handleVisionAwareAttach(img); err != nil {
		t.Fatalf("attach with nil model failed: %v", err)
	}
	o := out.String()
	if !strings.Contains(o, "could not be determined") && !strings.Contains(o, "No vision-capable") {
		t.Errorf("expected unknown handling, got:\n%s", o)
	}
	if len(repl.Attachments) != 1 {
		t.Error("image should be preserved for unknown")
	}
	// Ensure block on send when unknown and no vision installed
	repl.Attachments = []ImageAttachment{{Path: img, Filename: "a.png", Mime: vision.GetMimeType(img), Size: 100}}
	repl.Runtime = runtime.NewMockRuntime(runtime.MockConfig{})
	// Need a dummy model to satisfy runAgent's model nil check? runAgent returns "No local model" when Model nil, not vision error.
	// So set a model with empty ID to trigger unknown path
	repl.Model = &model.ModelMetadata{
		ID: "", Name: "unknown", Provider: "Test", Family: "test", ParameterCount: 1, Quantization: model.QuantizationQ4, FileSizeBytes: 1000, MinimumRAMBytes: 1000, RecommendedRAMBytes: 1000, ContextLength: 1000, Architecture: model.ArchitectureLlama, Capabilities: model.Capabilities{model.CapabilityCodeGeneration}, RuntimeCompatibility: []model.Runtime{model.RuntimeLlamaCPP}, Installed: true, InstallPath: "/tmp/x",
	}
	// But ID empty will be unknown
	_, err := repl.runAgent(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected unknown vision block")
	}
}

// TestMultipleImagesBlockedTogether verifies multiple images not partially sent
func TestMultipleImagesBlockedTogether(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.png")
	b := filepath.Join(tmp, "b.png")
	writeVisionPNG(t, a)
	writeVisionPNG(t, b)
	repl, _ := NewREPL(bytes.NewBufferString(""), &bytes.Buffer{}, &bytes.Buffer{})
	repl.Model = textOnlyModel()
	repl.ModelDir = t.TempDir()
	repl.Attachments = []ImageAttachment{
		{Path: a, Filename: "a.png", Mime: vision.GetMimeType(a), Size: 100},
		{Path: b, Filename: "b.png", Mime: vision.GetMimeType(b), Size: 100},
	}
	mock := runtime.NewMockRuntime(runtime.MockConfig{})
	calls := 0
	mock.GenerateFunc = func(ctx context.Context, req runtime.GenerateRequest) (*runtime.GenerateResponse, error) {
		calls++
		return &runtime.GenerateResponse{Text: "x", TokensGenerated: 1, FinishReason: "stop"}, nil
	}
	repl.Runtime = mock
	_, err := repl.runAgent(context.Background(), "hi")
	if err == nil {
		t.Fatal("should block")
	}
	if calls != 0 {
		t.Error("should not have called runtime with multiple images on text-only")
	}
	if len(repl.Attachments) != 2 {
		t.Error("both images should be preserved")
	}
}

// TestModelCapabilitiesExplicit verifies explicit Vision field is respected
func TestModelCapabilitiesExplicit(t *testing.T) {
	m := &model.ModelMetadata{
		ID: "custom-model", Name: "Custom", Provider: "P", Family: "F", ParameterCount: 1, Quantization: model.QuantizationQ4, FileSizeBytes: 1000, MinimumRAMBytes: 1000, RecommendedRAMBytes: 1000, ContextLength: 1000, Architecture: model.ArchitectureLlama, Capabilities: model.Capabilities{model.CapabilityCodeGeneration}, Vision: true, RuntimeCompatibility: []model.Runtime{model.RuntimeLlamaCPP}, Installed: true, InstallPath: "/tmp/x",
	}
	if !m.IsVisionCapable() {
		t.Error("explicit Vision true should be capable")
	}
	if !isVisionCapableModel(m) {
		t.Error("isVisionCapableModel should be true for explicit")
	}
	m2 := textOnlyModel()
	if m2.IsVisionCapable() {
		t.Error("text-only should not be vision capable")
	}
	if isVisionCapableModel(m2) {
		t.Error("heuristic should not make qwen2.5-coder vision")
	}
	// Heuristic fallback
	m3 := &model.ModelMetadata{ID: "llava-custom", Name: "LLaVA", Provider: "P", Family: "F", ParameterCount: 1, Quantization: model.QuantizationQ4, FileSizeBytes: 1000, MinimumRAMBytes: 1000, RecommendedRAMBytes: 1000, ContextLength: 1000, Architecture: model.ArchitectureLlama, Capabilities: model.Capabilities{model.CapabilityCodeGeneration}, Vision: false, RuntimeCompatibility: []model.Runtime{model.RuntimeLlamaCPP}, Installed: true, InstallPath: "/tmp/x"}
	if !isVisionCapableModel(m3) {
		t.Error("heuristic llava should be vision even if explicit false")
	}
}

// TestRecommendedVisionHardwareAware verifies recommended respects hardware
func TestRecommendedVisionHardwareAware(t *testing.T) {
	repl, _ := NewREPL(bytes.NewBufferString(""), &bytes.Buffer{}, &bytes.Buffer{})
	// Simulate low RAM hardware (1 GiB) should filter out larger models
	repl.Hardware.TotalRAMBytes = 1 * 1024 * 1024 * 1024
	// Use mock runtime that is compatible with all
	repl.Runtime = runtime.NewMockRuntime(runtime.MockConfig{})
	list := repl.recommendedVisionModels()
	if len(list) == 0 {
		t.Fatal("should have recommended vision models")
	}
	// With tiny RAM, moondream (1.2GB file, 2GB min) might still be filtered, but fallback will return all
	// At least ensure smallest model is present
	found := false
	for _, m := range list {
		if m.ID == "moondream-1.8b-q4" {
			found = true
			break
		}
	}
	if !found {
		t.Error("moondream should be in recommended for low RAM")
	}
}

// TestVisionBuiltInCatalog checks catalog contains vision models with correct capabilities
func TestVisionBuiltInCatalog(t *testing.T) {
	cat := model.BuiltInCatalog()
	var visionCount int
	for _, m := range cat {
		if m.IsVisionCapable() {
			visionCount++
			if !m.Capabilities.Has(model.CapabilityVision) {
				t.Errorf("vision model %q missing CapabilityVision", m.ID)
			}
			if !m.Capabilities.Has(model.CapabilityCodeGeneration) {
				t.Errorf("vision model %q should also have code_generation", m.ID)
			}
		}
	}
	if visionCount < 3 {
		t.Errorf("expected at least 3 vision models in catalog, got %d", visionCount)
	}
}
