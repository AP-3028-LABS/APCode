// Package model defines APCode's model abstraction and metadata.
//
// A future milestone will introduce APCode-Tiny, a small efficient model
// for low-resource laptops, alongside stronger models for capable ones.
// This milestone establishes the model metadata and registry.
package model

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrNotImplemented is returned by operations that require a real model.
var ErrNotImplemented = errors.New("model: not implemented")

// Kind classifies models by target hardware tier.
type Kind int

const (
	// Tiny targets low-resource laptops.
	Tiny Kind = iota
	// Standard targets capable laptops.
	Standard
)

// Model is a language model that can generate completions locally.
type Model interface {
	// Name returns the model's identifier.
	Name() string
	// Kind reports the hardware tier the model targets.
	Kind() Kind
	// Complete generates text for the given prompt.
	Complete(ctx context.Context, prompt string) (string, error)
}

// Registry resolves which model best fits the current machine.
type Registry interface {
	// Select returns the most suitable model for the machine profile.
	Select() (Model, error)
}

// Capability represents a model capability.
type Capability string

const (
	CapabilityCodeGeneration  Capability = "code_generation"
	CapabilityCodeCompletion  Capability = "code_completion"
	CapabilityCodeExplanation Capability = "code_explanation"
	CapabilityRefactoring     Capability = "refactoring"
	CapabilityDebugging       Capability = "debugging"
	CapabilityToolCalling     Capability = "tool_calling"
	CapabilityReasoning       Capability = "reasoning"
	CapabilityVision          Capability = "vision"
)

// ModelCapabilities captures explicit hardware-facing capabilities for routing.
// It provides a clear boolean representation for vision support beyond
// substring heuristics.
type ModelCapabilities struct {
	Vision bool
}

// Capabilities is a set of capabilities.
type Capabilities []Capability

// Has returns true if all given capabilities are present.
func (c Capabilities) Has(caps ...Capability) bool {
	for _, cap := range caps {
		found := false
		for _, c := range c {
			if c == cap {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Quantization represents model quantization.
type Quantization string

const (
	QuantizationFP16 Quantization = "fp16"
	QuantizationQ8   Quantization = "q8"
	QuantizationQ6   Quantization = "q6"
	QuantizationQ5   Quantization = "q5"
	QuantizationQ4   Quantization = "q4"
	QuantizationQ3   Quantization = "q3"
)

// Runtime represents a local inference runtime.
type Runtime string

const (
	RuntimeLlamaCPP Runtime = "llama.cpp"
	RuntimeOllama   Runtime = "ollama"
	RuntimeMLX      Runtime = "mlx"
)

// Architecture represents model architecture.
type Architecture string

const (
	ArchitectureLlama     Architecture = "llama"
	ArchitectureMistral   Architecture = "mistral"
	ArchitecturePhi       Architecture = "phi"
	ArchitectureGemma     Architecture = "gemma"
	ArchitectureCodeLlama Architecture = "codellama"
	ArchitectureDeepSeek  Architecture = "deepseek"
	ArchitectureQwen      Architecture = "qwen"
)

// ModelMetadata contains static metadata about a model.
// This is metadata only - it does not imply the model file is installed.
type ModelMetadata struct {
	// ID is a unique identifier for this model entry.
	ID string

	// Name is the human-readable model name.
	Name string

	// Provider is the model provider/organization.
	Provider string

	// Family is the model family name.
	Family string

	// ParameterCount is the approximate parameter count in billions.
	ParameterCount float64

	// Quantization is the model quantization.
	Quantization Quantization

	// FileSizeBytes is the model file size in bytes.
	FileSizeBytes uint64

	// MinimumRAMBytes is the minimum RAM required to run the model.
	MinimumRAMBytes uint64

	// RecommendedRAMBytes is the recommended RAM for good performance.
	RecommendedRAMBytes uint64

	// ContextLength is the maximum context window in tokens.
	ContextLength int

	// Architecture is the model architecture.
	Architecture Architecture

	// Capabilities is the set of capabilities this model supports.
	Capabilities Capabilities

	// Vision indicates explicit vision/multimodal capability.
	// When true the model supports image inputs; when false it is text-only.
	// This field provides an explicit boolean beyond substring heuristics.
	Vision bool

	// RuntimeCompatibility lists compatible local inference runtimes.
	RuntimeCompatibility []Runtime

	// Installed indicates whether the model file is present locally.
	Installed bool

	// InstallPath is the local filesystem path if installed.
	InstallPath string
}

// Validate checks the model metadata for validity.
func (m *ModelMetadata) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("model: ID cannot be empty")
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("model: Name cannot be empty")
	}
	if strings.TrimSpace(m.Provider) == "" {
		return errors.New("model: Provider cannot be empty")
	}
	if strings.TrimSpace(m.Family) == "" {
		return errors.New("model: Family cannot be empty")
	}
	if m.ParameterCount <= 0 {
		return errors.New("model: ParameterCount must be positive")
	}
	if m.FileSizeBytes == 0 {
		return errors.New("model: FileSizeBytes must be positive")
	}
	if m.MinimumRAMBytes == 0 {
		return errors.New("model: MinimumRAMBytes must be positive")
	}
	if m.RecommendedRAMBytes < m.MinimumRAMBytes {
		return errors.New("model: RecommendedRAMBytes must be >= MinimumRAMBytes")
	}
	if m.ContextLength <= 0 {
		return errors.New("model: ContextLength must be positive")
	}
	if strings.TrimSpace(string(m.Architecture)) == "" {
		return errors.New("model: Architecture cannot be empty")
	}
	if len(m.Capabilities) == 0 {
		return errors.New("model: at least one Capability is required")
	}
	for _, cap := range m.Capabilities {
		if !isValidCapability(cap) {
			return fmt.Errorf("model: invalid capability %q", cap)
		}
	}
	if len(m.RuntimeCompatibility) == 0 {
		return errors.New("model: at least one RuntimeCompatibility is required")
	}
	for _, rt := range m.RuntimeCompatibility {
		if !isValidRuntime(rt) {
			return fmt.Errorf("model: invalid runtime %q", rt)
		}
	}
	if m.Quantization != "" && !isValidQuantization(m.Quantization) {
		return fmt.Errorf("model: invalid quantization %q", m.Quantization)
	}
	if m.Installed && strings.TrimSpace(m.InstallPath) == "" {
		return errors.New("model: InstallPath required when Installed is true")
	}
	return nil
}

func isValidCapability(c Capability) bool {
	valid := map[Capability]bool{
		CapabilityCodeGeneration:  true,
		CapabilityCodeCompletion:  true,
		CapabilityCodeExplanation: true,
		CapabilityRefactoring:     true,
		CapabilityDebugging:       true,
		CapabilityToolCalling:     true,
		CapabilityReasoning:       true,
		CapabilityVision:          true,
	}
	return valid[c]
}

// IsVisionCapable reports whether the metadata explicitly supports vision.
// It checks the Vision field and the Vision capability for an explicit
// determination without relying on substring heuristics.
func (m *ModelMetadata) IsVisionCapable() bool {
	if m == nil {
		return false
	}
	if m.Vision {
		return true
	}
	if m.Capabilities.Has(CapabilityVision) {
		return true
	}
	return false
}

// VisionCapabilityKnown reports whether vision capability is explicitly known
// via metadata (Vision field or Vision capability). When false the caller
// should fall back to heuristic substring matching or treat as unknown.
func (m *ModelMetadata) VisionCapabilityKnown() bool {
	if m == nil {
		return false
	}
	if m.Vision {
		return true
	}
	if m.Capabilities.Has(CapabilityVision) {
		return true
	}
	// Explicit text-only is known when Vision is false and capability not present
	// but model has other capabilities; we treat any non-nil model with ID as known
	// for the purpose of distinguishing unknown (nil) vs known text-only.
	// To keep explicit vs heuristic distinct, we consider Vision==false plus
	// non-vision capability set as known text-only.
	if strings.TrimSpace(m.ID) != "" {
		return true
	}
	return false
}

// GetModelCapabilities returns the explicit ModelCapabilities view.
func (m *ModelMetadata) GetModelCapabilities() ModelCapabilities {
	if m == nil {
		return ModelCapabilities{Vision: false}
	}
	return ModelCapabilities{Vision: m.IsVisionCapable()}
}

func isValidQuantization(q Quantization) bool {
	valid := map[Quantization]bool{
		QuantizationFP16: true,
		QuantizationQ8:   true,
		QuantizationQ6:   true,
		QuantizationQ5:   true,
		QuantizationQ4:   true,
		QuantizationQ3:   true,
	}
	return valid[q]
}

func isValidRuntime(r Runtime) bool {
	valid := map[Runtime]bool{
		RuntimeLlamaCPP: true,
		RuntimeOllama:   true,
		RuntimeMLX:      true,
	}
	return valid[r]
}

// ModelRegistry is a thread-safe in-memory model registry.
type ModelRegistry struct {
	mu     sync.RWMutex
	models map[string]*ModelMetadata
}

// NewModelRegistry creates a new model registry.
func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{
		models: make(map[string]*ModelMetadata),
	}
}

// Add adds a model to the registry.
func (r *ModelRegistry) Add(m *ModelMetadata) error {
	if err := m.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.models[m.ID]; exists {
		return fmt.Errorf("model: duplicate ID %q", m.ID)
	}
	r.models[m.ID] = m
	return nil
}

// Get retrieves a model by ID.
func (r *ModelRegistry) Get(id string) (*ModelMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[id]
	return m, ok
}

// Remove removes a model by ID.
func (r *ModelRegistry) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.models[id]; !exists {
		return false
	}
	delete(r.models, id)
	return true
}

// List returns all models sorted by ID.
func (r *ModelRegistry) List() []*ModelMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*ModelMetadata, 0, len(r.models))
	for _, m := range r.models {
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// FindByCapability returns models that have all the given capabilities.
func (r *ModelRegistry) FindByCapability(caps ...Capability) []*ModelMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*ModelMetadata
	for _, m := range r.models {
		if m.Capabilities.Has(caps...) {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// FindInstalled returns only installed models.
func (r *ModelRegistry) FindInstalled() []*ModelMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*ModelMetadata
	for _, m := range r.models {
		if m.Installed {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// Count returns the number of models in the registry.
func (r *ModelRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.models)
}

// BuiltInCatalog returns a catalog of known coding models (metadata only).
// These are metadata entries only - no model files are downloaded or assumed to exist.
func BuiltInCatalog() []*ModelMetadata {
	return []*ModelMetadata{
		{
			ID:                   "codellama-7b-q4",
			Name:                 "CodeLlama 7B Q4",
			Provider:             "Meta",
			Family:               "CodeLlama",
			ParameterCount:       7,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        4_000_000_000,
			MinimumRAMBytes:      6_000_000_000,
			RecommendedRAMBytes:  8_000_000_000,
			ContextLength:        16384,
			Architecture:         ArchitectureCodeLlama,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityCodeExplanation, CapabilityRefactoring},
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "codellama-13b-q4",
			Name:                 "CodeLlama 13B Q4",
			Provider:             "Meta",
			Family:               "CodeLlama",
			ParameterCount:       13,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        7_500_000_000,
			MinimumRAMBytes:      10_000_000_000,
			RecommendedRAMBytes:  16_000_000_000,
			ContextLength:        16384,
			Architecture:         ArchitectureCodeLlama,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityCodeExplanation, CapabilityRefactoring, CapabilityReasoning},
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "deepseek-coder-6.7b-q4",
			Name:                 "DeepSeek Coder 6.7B Q4",
			Provider:             "DeepSeek",
			Family:               "DeepSeek Coder",
			ParameterCount:       6.7,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        3_800_000_000,
			MinimumRAMBytes:      5_500_000_000,
			RecommendedRAMBytes:  8_000_000_000,
			ContextLength:        16384,
			Architecture:         ArchitectureDeepSeek,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityCodeExplanation, CapabilityRefactoring, CapabilityToolCalling},
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "phi-3-mini-q4",
			Name:                 "Phi-3 Mini Q4",
			Provider:             "Microsoft",
			Family:               "Phi",
			ParameterCount:       3.8,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        2_200_000_000,
			MinimumRAMBytes:      3_000_000_000,
			RecommendedRAMBytes:  4_000_000_000,
			ContextLength:        128000,
			Architecture:         ArchitecturePhi,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityCodeExplanation, CapabilityReasoning},
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama, RuntimeMLX},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "gemma-2b-q4",
			Name:                 "Gemma 2B Q4",
			Provider:             "Google",
			Family:               "Gemma",
			ParameterCount:       2,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        1_500_000_000,
			MinimumRAMBytes:      2_000_000_000,
			RecommendedRAMBytes:  3_000_000_000,
			ContextLength:        8192,
			Architecture:         ArchitectureGemma,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityCodeExplanation},
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama, RuntimeMLX},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "qwen2.5-coder-7b-q4",
			Name:                 "Qwen2.5-Coder 7B Q4",
			Provider:             "Qwen",
			Family:               "Qwen2.5-Coder",
			ParameterCount:       7,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        4_200_000_000,
			MinimumRAMBytes:      6_000_000_000,
			RecommendedRAMBytes:  8_000_000_000,
			ContextLength:        32768,
			Architecture:         ArchitectureQwen,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityCodeExplanation, CapabilityRefactoring, CapabilityToolCalling, CapabilityReasoning},
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama},
			Installed:            false,
			InstallPath:          "",
		},
		// Vision-capable models (explicit Vision flag + Vision capability).
		{
			ID:                   "qwen2-vl-7b-q4",
			Name:                 "Qwen2-VL 7B Q4",
			Provider:             "Qwen",
			Family:               "Qwen2-VL",
			ParameterCount:       7,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        4_500_000_000,
			MinimumRAMBytes:      6_000_000_000,
			RecommendedRAMBytes:  8_000_000_000,
			ContextLength:        32768,
			Architecture:         ArchitectureQwen,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityCodeExplanation, CapabilityVision},
			Vision:               true,
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "llava-7b-q4",
			Name:                 "LLaVA 7B Q4",
			Provider:             "LLaVA",
			Family:               "LLaVA",
			ParameterCount:       7,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        4_000_000_000,
			MinimumRAMBytes:      6_000_000_000,
			RecommendedRAMBytes:  8_000_000_000,
			ContextLength:        4096,
			Architecture:         ArchitectureLlama,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityVision},
			Vision:               true,
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "bakllava-7b-q4",
			Name:                 "BakLLaVA 7B Q4",
			Provider:             "BakLLaVA",
			Family:               "BakLLaVA",
			ParameterCount:       7,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        4_200_000_000,
			MinimumRAMBytes:      6_000_000_000,
			RecommendedRAMBytes:  8_000_000_000,
			ContextLength:        4096,
			Architecture:         ArchitectureLlama,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityVision},
			Vision:               true,
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "moondream-1.8b-q4",
			Name:                 "Moondream 1.8B Q4",
			Provider:             "Moondream",
			Family:               "Moondream",
			ParameterCount:       1.8,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        1_200_000_000,
			MinimumRAMBytes:      2_000_000_000,
			RecommendedRAMBytes:  3_000_000_000,
			ContextLength:        2048,
			Architecture:         ArchitecturePhi,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityVision},
			Vision:               true,
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama},
			Installed:            false,
			InstallPath:          "",
		},
		{
			ID:                   "llava-phi-3b-q4",
			Name:                 "LLaVA-Phi 3B Q4",
			Provider:             "LLaVA",
			Family:               "LLaVA-Phi",
			ParameterCount:       3,
			Quantization:         QuantizationQ4,
			FileSizeBytes:        2_000_000_000,
			MinimumRAMBytes:      3_000_000_000,
			RecommendedRAMBytes:  4_000_000_000,
			ContextLength:        128000,
			Architecture:         ArchitecturePhi,
			Capabilities:         Capabilities{CapabilityCodeGeneration, CapabilityCodeCompletion, CapabilityVision},
			Vision:               true,
			RuntimeCompatibility: []Runtime{RuntimeLlamaCPP, RuntimeOllama, RuntimeMLX},
			Installed:            false,
			InstallPath:          "",
		},
	}
}

// IsVisionModel reports whether modelID indicates a multimodal vision model
// (LLaVA, BakLLaVA, Qwen2-VL, etc.) by explicit registry lookup or substring match.
// This is the heuristic fallback for IDs not in the explicit registry.
func IsVisionModel(modelID string) bool {
	// First check explicit registry entries.
	for _, m := range BuiltInCatalog() {
		if m.ID == modelID && m.IsVisionCapable() {
			return true
		}
	}
	// Fallback to substring matching for external/Ollama IDs.
	lower := strings.ToLower(strings.TrimSpace(modelID))
	if lower == "" {
		return false
	}
	for _, sub := range visionSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// visionSubstrings contains lowercase substrings that indicate vision capability
// for heuristic matching when explicit metadata is unavailable.
var visionSubstrings = []string{
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
