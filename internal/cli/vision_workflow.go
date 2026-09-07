package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"apcode/internal/config"
	"apcode/internal/hardware"
	"apcode/internal/localmodel"
	"apcode/internal/model"
	"apcode/internal/recommendation"
	"apcode/internal/tui"
	"apcode/internal/vision"
)

// isVisionCapableModel reports whether the given model supports vision input
// using explicit metadata first, falling back to substring heuristics for
// external or unstructured model IDs.
func isVisionCapableModel(m *model.ModelMetadata) bool {
	if m == nil {
		return false
	}
	if m.IsVisionCapable() {
		return true
	}
	return vision.IsVisionModel(m.ID)
}

// visionCapabilityStatus returns the vision capability status for the active model.
// Unknown is returned when model is nil or ID empty.
func (r *REPL) visionCapabilityStatus() vision.VisionCapabilityStatus {
	if r.Model == nil {
		return vision.VisionUnknown
	}
	if r.Model.IsVisionCapable() {
		return vision.VisionCapable
	}
	if vision.IsVisionModel(r.Model.ID) {
		return vision.VisionCapable
	}
	// Explicit text-only when model known
	if strings.TrimSpace(r.Model.ID) != "" {
		return vision.VisionNotCapable
	}
	return vision.VisionUnknown
}

// installedVisionModels returns vision-capable models that are installed locally
// and compatible (when runtime present). It uses the localmodel manager and
// falls back to registry installed flags.
func (r *REPL) installedVisionModels() []*model.ModelMetadata {
	dir := r.ModelDir
	if strings.TrimSpace(dir) == "" {
		dir = config.DefaultModelDir()
	}
	registry := model.NewModelRegistry()
	for _, m := range model.BuiltInCatalog() {
		_ = registry.Add(m)
	}
	mgr, err := localmodel.NewManager(dir, registry)
	if err != nil {
		// fallback to registry installed flags if manager unavailable
		var out []*model.ModelMetadata
		for _, m := range registry.FindInstalled() {
			if isVisionCapableModel(m) {
				out = append(out, m)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out
	}
	installed := mgr.ListInstalled()
	var visionModels []*model.ModelMetadata
	for _, m := range installed {
		if isVisionCapableModel(m) {
			// Filter by runtime compatibility when runtime known
			if r.Runtime != nil && !r.Runtime.IsCompatible(m) {
				continue
			}
			visionModels = append(visionModels, m)
		}
	}
	sort.Slice(visionModels, func(i, j int) bool { return visionModels[i].ID < visionModels[j].ID })
	return visionModels
}

// recommendedVisionModels returns vision models filtered and scored for the
// current hardware, using the same hardware-aware recommendation logic.
// It does not require installation; used when no vision model is installed.
func (r *REPL) recommendedVisionModels() []*model.ModelMetadata {
	// Gather vision models from catalog
	var visionCandidates []*model.ModelMetadata
	for _, m := range model.BuiltInCatalog() {
		if isVisionCapableModel(m) {
			visionCandidates = append(visionCandidates, m)
		}
	}
	// If hardware detection failed, return all sorted by size (smallest first)
	hw := r.Hardware
	// Try to evaluate memory fit; filter out incompatible (requires > available)
	var filtered []*model.ModelMetadata
	for _, m := range visionCandidates {
		fit := recommendation.EvaluateMemoryFit(hw, m)
		if fit.Status == recommendation.RAMStatusIncompatible {
			continue
		}
		// Also check runtime compatibility when runtime known
		if r.Runtime != nil && !r.Runtime.IsCompatible(m) {
			continue
		}
		filtered = append(filtered, m)
	}
	if len(filtered) == 0 {
		// If all filtered out (e.g., hardware unknown or too small), fall back to all vision models sorted by size
		filtered = visionCandidates
	}
	// Score via recommender for stable ordering (reuse ranking)
	rec := recommendation.NewRecommender()
	input := recommendation.RecommendationInput{
		Hardware:            hw,
		Models:              filtered,
		RequestedCapability: model.CapabilityVision,
		Preference:          recommendation.PreferenceBalanced,
	}
	// If hardware profile empty (e.g., in tests OS empty), skip recommendation and just sort by size
	if hw.OS == "" && hw.Arch == "" {
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].FileSizeBytes != filtered[j].FileSizeBytes {
				return filtered[i].FileSizeBytes < filtered[j].FileSizeBytes
			}
			return filtered[i].ID < filtered[j].ID
		})
		return filtered
	}
	result, err := rec.Recommend(input)
	if err != nil {
		// Fallback sort by size
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].FileSizeBytes != filtered[j].FileSizeBytes {
				return filtered[i].FileSizeBytes < filtered[j].FileSizeBytes
			}
			return filtered[i].ID < filtered[j].ID
		})
		return filtered
	}
	ordered := make([]*model.ModelMetadata, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		ordered = append(ordered, c.Model)
	}
	// Append rejected (incompatible) at end if any, for completeness
	for _, c := range result.Rejected {
		ordered = append(ordered, c.Model)
	}
	return ordered
}

// promptVisionModelSelection shows available vision models and asks the user to
// choose one. It reads from r.reader and returns the chosen model or nil if cancelled.
// It preserves the REPL's In/Out abstractions and does not modify model state.
func (r *REPL) promptVisionModelSelection(installed []*model.ModelMetadata) (*model.ModelMetadata, bool) {
	if len(installed) == 0 {
		return nil, false
	}
	// Render a box similar to spec.
	var lines []string
	currentID := ""
	if r.Model != nil {
		currentID = r.Model.ID
	} else {
		currentID = "(none)"
	}
	lines = append(lines, tui.Muted("Current model"))
	lines = append(lines, "  "+currentID)
	lines = append(lines, "")
	lines = append(lines, tui.Warning("⚠ This model does not support vision."))
	lines = append(lines, "")
	lines = append(lines, tui.Muted("Available vision models:"))
	for i, m := range installed {
		// Show size hint for hardware-aware context
		lines = append(lines, fmt.Sprintf("  %d. %s %s", i+1, m.ID, tui.Muted(fmt.Sprintf("(%.1fB)", m.ParameterCount))))
	}
	lines = append(lines, "")
	lines = append(lines, tui.Muted("Choose a model [1-2] · Esc cancel"))
	// Use Box with title
	box := tui.Box("APCode · Image Input", lines)
	fmt.Fprintln(r.Out, box)

	// Also provide a one-liner prompt for input
	fmt.Fprint(r.Out, tui.Muted("Select vision model [1-")+fmt.Sprint(len(installed))+tui.Muted("] or Esc to cancel: "))
	if r.reader == nil {
		r.reader = bufio.NewReader(r.In)
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		fmt.Fprintln(r.Out, tui.Muted("Image selection cancelled."))
		return nil, false
	}
	sel := strings.TrimSpace(strings.ReplaceAll(line, "\x1b", ""))
	lower := strings.ToLower(sel)
	if sel == "" || lower == "esc" || lower == "q" || lower == "quit" || lower == "cancel" || lower == "n" || lower == "no" {
		fmt.Fprintln(r.Out, tui.Muted("Vision model selection cancelled. Image remains attached but will be blocked until a vision model is selected."))
		return nil, false
	}
	// Support "y" to auto-select first model
	if lower == "y" || lower == "yes" {
		return installed[0], true
	}
	// Try numeric selection
	var idx int
	if _, err := fmt.Sscanf(sel, "%d", &idx); err == nil {
		if idx >= 1 && idx <= len(installed) {
			return installed[idx-1], true
		}
	}
	// Try ID match
	for _, m := range installed {
		if strings.EqualFold(m.ID, sel) {
			return m, true
		}
	}
	fmt.Fprintln(r.Out, tui.Warning("Invalid selection. Image remains attached. Use /image again or manually switch model."))
	return nil, false
}

// switchToVisionModel performs the visible model switch, preserving attachments.
// It prints the switching banner and updates r.Model.
func (r *REPL) switchToVisionModel(target *model.ModelMetadata) {
	if target == nil || r.Model == nil {
		// If current model nil, just set
		if target != nil {
			prev := ""
			if r.Model != nil {
				prev = r.Model.ID
				fmt.Fprintln(r.Out, tui.Box("Model Switch", []string{
					tui.Muted("Switching model"),
					"  " + prev,
					tui.Muted("       ↓"),
					"  " + target.ID,
				}))
			}
			r.Model = target
			fmt.Fprintf(r.Out, "%s Vision model selected: %s\n", tui.Success("✓"), target.ID)
		}
		return
	}
	if target.ID == r.Model.ID {
		return
	}
	prevID := r.Model.ID
	fmt.Fprintln(r.Out, tui.Box("Model Switch", []string{
		tui.Muted("Switching model"),
		"  " + prevID,
		tui.Muted("       ↓"),
		"  " + target.ID,
	}))
	r.Model = target
	fmt.Fprintf(r.Out, "%s Vision model selected: %s\n", tui.Success("✓"), target.ID)
}

// handleVisionAwareAttach validates the image and handles vision capability workflow
// before actually attaching. It preserves the image and never silently drops it.
func (r *REPL) handleVisionAwareAttach(path string) error {
	if err := vision.ValidateImageFile(path); err != nil {
		return err
	}
	// Determine vision capability of active model
	status := r.visionCapabilityStatus()
	isVision := status == vision.VisionCapable

	if isVision {
		// Case A: Active model supports vision
		return r.attachImageWithVisionSuccess(path)
	}

	// Unknown capability handling
	if status == vision.VisionUnknown {
		// Show unknown message but still proceed to check installed models
		fmt.Fprintln(r.Out, tui.Box("APCode · Image Input", []string{
			tui.Muted("Current model"),
			"  (unknown)",
			"",
			tui.Warning("? Vision capability could not be determined."),
			tui.Muted("Image attachments require a vision-capable model."),
			tui.Muted("Proceeding with available options..."),
		}))
	}

	// Text-only or unknown: check for installed vision models
	installed := r.installedVisionModels()
	if len(installed) > 0 {
		// Case B: Text-only but vision model(s) available - prompt to switch
		currentID := ""
		if r.Model != nil {
			currentID = r.Model.ID
		} else {
			currentID = "(none)"
		}
		fmt.Fprintln(r.Out, tui.Box("APCode · Image Input", []string{
			tui.Muted("Current model"),
			"  " + currentID,
			"",
			tui.Warning("⚠ This model does not support image input."),
			tui.Muted("Attachments require a vision-capable model."),
		}))
		// List available models inline for immediate visibility
		fmt.Fprintln(r.Out, tui.Muted("Available vision models:"))
		for i, m := range installed {
			fmt.Fprintf(r.Out, "  %d. %s\n", i+1, m.ID)
		}
		// Prompt
		chosen, ok := r.promptVisionModelSelection(installed)
		if ok && chosen != nil {
			r.switchToVisionModel(chosen)
			// Now attach with success
			if err := r.attachImageWithVisionSuccess(path); err != nil {
				return err
			}
			return nil
		}
		// User cancelled: still attach but warn that send will be blocked
		fmt.Fprintln(r.Out, tui.Warning("⚠ No vision model selected."))
		fmt.Fprintln(r.Out, tui.Muted("Image will be held but not sent to a text-only model. Select a vision model before sending."))
		// Attach anyway (preserve)
		return r.attachImagePreserve(path, true)
	}

	// Case C: No vision model installed
	return r.handleNoVisionModelCase(path)
}

// attachImageWithVisionSuccess attaches and shows success messages for vision-capable path
func (r *REPL) attachImageWithVisionSuccess(path string) error {
	if err := vision.ValidateImageFile(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	r.Attachments = append(r.Attachments, ImageAttachment{
		Path:     path,
		Filename: filepath.Base(path),
		Mime:     vision.GetMimeType(path),
		Size:     info.Size(),
	})
	// Success UX
	fmt.Fprintf(r.Out, "%s Active model supports image input.\n", tui.Success("✓"))
	fmt.Fprintf(r.Out, "%s Attached %s\n", tui.Success("✓"), tui.AttachmentChips(r.attachedPaths()))
	fmt.Fprintln(r.Out, tui.Muted("The image will be sent with your next prompt and cleared after. Use /clear to remove it."))
	return nil
}

// attachImagePreserve attaches without vision success banner, used when model selection cancelled
// or when no vision model installed but we still preserve attachment.
func (r *REPL) attachImagePreserve(path string, showWarning bool) error {
	if err := vision.ValidateImageFile(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	r.Attachments = append(r.Attachments, ImageAttachment{
		Path:     path,
		Filename: filepath.Base(path),
		Mime:     vision.GetMimeType(path),
		Size:     info.Size(),
	})
	if showWarning {
		modelID := ""
		if r.Model != nil {
			modelID = r.Model.ID
		} else {
			modelID = "(no model)"
		}
		fmt.Fprintf(r.Out, "%s Current model %q does not support image input.\n", tui.Warning("⚠"), modelID)
	}
	fmt.Fprintf(r.Out, "%s Attached %s\n", tui.Success("✓"), tui.AttachmentChips(r.attachedPaths()))
	fmt.Fprintln(r.Out, tui.Muted("The image will be held until a vision model is available. Use /clear to remove it."))
	return nil
}

// handleNoVisionModelCase handles the scenario where no vision model is installed
func (r *REPL) handleNoVisionModelCase(path string) error {
	currentID := ""
	if r.Model != nil {
		currentID = r.Model.ID
	} else {
		currentID = "(none)"
	}
	lines := []string{
		tui.Muted("Current model:"),
		"  " + currentID,
		"",
		tui.Warning("⚠ No vision-capable local model is installed."),
		tui.Muted("Image attachments require a vision-capable model."),
		"",
		tui.Muted("Recommended models:"),
	}
	recommended := r.recommendedVisionModels()
	// Show up to 3 recommended
	limit := 3
	if len(recommended) < limit {
		limit = len(recommended)
	}
	if limit == 0 {
		// Fallback to static list if recommendation unavailable
		for _, id := range vision.RecommendedVisionModelIDs[:3] {
			lines = append(lines, "  "+id)
		}
	} else {
		for i := 0; i < limit; i++ {
			m := recommended[i]
			lines = append(lines, fmt.Sprintf("  %s %s", m.ID, tui.Muted(fmt.Sprintf("(%.1fB, %s)", m.ParameterCount, formatBytesVision(m.FileSizeBytes)))))
		}
	}
	lines = append(lines, "")
	lines = append(lines, tui.Muted("Install through APCode's model workflow:"))
	// Determine actual install command from main.go: "apcode models install <id>"
	exampleID := "qwen2-vl-7b-q4"
	if len(recommended) > 0 {
		exampleID = recommended[0].ID
	} else if len(vision.RecommendedVisionModelIDs) > 0 {
		exampleID = vision.RecommendedVisionModelIDs[0]
	}
	lines = append(lines, "  "+tui.Primary("apcode models install "+exampleID))
	lines = append(lines, tui.Muted("Or list all: apcode models"))
	if r.Hardware.TotalRAMBytes > 0 || r.Hardware.GPU.Known {
		// Show hardware-aware hint
		lines = append(lines, "")
		lines = append(lines, tui.Muted("Your hardware:"))
		if r.Hardware.TotalRAMBytes > 0 {
			lines = append(lines, fmt.Sprintf("  RAM: %s", formatBytesVision(r.Hardware.TotalRAMBytes)))
		}
		if r.Hardware.GPU.Known {
			gpuLine := fmt.Sprintf("  GPU: %s", r.Hardware.GPU.Name)
			if r.Hardware.GPU.VRAMKnown && r.Hardware.GPU.VRAMBytes > 0 {
				gpuLine += fmt.Sprintf(" (%s VRAM)", formatBytesVision(r.Hardware.GPU.VRAMBytes))
			}
			lines = append(lines, gpuLine)
		}
	}

	fmt.Fprintln(r.Out, tui.Box("APCode · Image Input", lines))

	// Check hardware compatibility for recommended model (show warning if none fits)
	if len(recommended) > 0 {
		unsupported := true
		for _, m := range recommended {
			if m.MinimumRAMBytes <= r.Hardware.TotalRAMBytes || r.Hardware.TotalRAMBytes == 0 {
				unsupported = false
				break
			}
		}
		if unsupported && r.Hardware.TotalRAMBytes > 0 {
			fmt.Fprintln(r.Out, tui.Warning("⚠ None of the recommended vision models fit available RAM; consider a smaller model like moondream-1.8b-q4."))
		}
	}

	// Attach anyway but warn that send will be blocked
	return r.attachImagePreserve(path, false)
}

// ensureVisionModelForRequest checks before sending a multimodal request that the
// active model supports vision. If not, it blocks the request with an actionable
// message and does NOT call the runtime. It returns nil when safe to send.
func (r *REPL) ensureVisionModelForRequest() error {
	if len(r.Attachments) == 0 {
		return nil
	}
	status := r.visionCapabilityStatus()
	if status == vision.VisionCapable {
		return nil
	}
	// Unknown or NotCapable -> block
	installed := r.installedVisionModels()
	if status == vision.VisionUnknown {
		if len(installed) > 0 {
			fmt.Fprintln(r.Out, tui.Box("Vision Check", []string{
				tui.Warning("? Vision capability could not be determined."),
				tui.Muted("Image attachments require a vision-capable model."),
				"",
				tui.Muted("Available vision models:"),
			}))
			for i, m := range installed {
				fmt.Fprintf(r.Out, "  %d. %s\n", i+1, m.ID)
			}
			fmt.Fprintln(r.Out, tui.Muted("Select a vision model before sending. Example: apcode models install "+installed[0].ID))
			return fmt.Errorf("vision capability unknown: select a vision model before sending images")
		}
		fmt.Fprintln(r.Out, tui.Box("Vision Check", []string{
			tui.Warning("? Vision capability could not be determined."),
			tui.Muted("Image attachments require a vision-capable model."),
			tui.Muted("No vision model installed."),
			"  " + tui.Primary("apcode models install qwen2-vl-7b-q4"),
		}))
		return fmt.Errorf("vision capability unknown and no vision model installed")
	}
	// NotCapable
	if len(installed) > 0 {
		lines := []string{
			tui.Warning("⚠ Current model \"" + safeModelID(r.Model) + "\" does not support image input."),
			tui.Muted("Attachments require a vision-capable model."),
			"",
			tui.Muted("Available vision models:"),
		}
		for i, m := range installed {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, m.ID))
		}
		lines = append(lines, "")
		lines = append(lines, tui.Muted("Use vision model \""+installed[0].ID+"\" for this request? [Y/n]"))
		// For non-interactive blocking, we show message and return error.
		// If interactive, we could prompt here, but to avoid blocking send without explicit user action,
		// we return actionable error and let caller decide to switch.
		fmt.Fprintln(r.Out, tui.Box("Vision Check", lines))
		// Auto-prompt if interactive? We attempt prompt when in ensureVision path via runAgent
		// Try interactive prompt once
		chosen, ok := r.promptVisionModelSelection(installed)
		if ok && chosen != nil {
			r.switchToVisionModel(chosen)
			// Re-check capability after switch
			if r.visionCapabilityStatus() == vision.VisionCapable {
				fmt.Fprintf(r.Out, "%s Vision model selected for this request.\n", tui.Success("✓"))
				return nil
			}
		}
		return fmt.Errorf("text-only model cannot process images; available vision models: %s", strings.Join(modelIDs(installed), ", "))
	}
	// No vision model installed
	lines := []string{
		tui.Warning("⚠ No vision-capable local model is installed."),
		tui.Muted("Current model: " + safeModelID(r.Model)),
		tui.Muted("Image attachments require a vision-capable model."),
		"",
		tui.Muted("Recommended models:"),
	}
	rec := r.recommendedVisionModels()
	limit := 3
	if len(rec) < limit {
		limit = len(rec)
	}
	if limit == 0 {
		for _, id := range vision.RecommendedVisionModelIDs[:3] {
			lines = append(lines, "  "+id)
		}
	} else {
		for i := 0; i < limit; i++ {
			lines = append(lines, "  "+rec[i].ID)
		}
	}
	exampleID := "qwen2-vl-7b-q4"
	if len(rec) > 0 {
		exampleID = rec[0].ID
	}
	lines = append(lines, "")
	lines = append(lines, tui.Muted("Install: apcode models install "+exampleID))
	fmt.Fprintln(r.Out, tui.Box("Vision Check", lines))
	return fmt.Errorf("no vision-capable model installed; install with: apcode models install %s", exampleID)
}

func safeModelID(m *model.ModelMetadata) string {
	if m == nil {
		return "(none)"
	}
	return m.ID
}

func modelIDs(models []*model.ModelMetadata) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out
}

func formatBytesVision(bytes uint64) string {
	const (
		kib = 1024
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case bytes >= gib:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/gib)
	case bytes >= mib:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/mib)
	case bytes >= kib:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/kib)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func init() {
	_ = hardware.HardwareProfile{}
	_ = recommendation.Recommender{}
	_ = vision.IsVisionModel
}
