// Package recommendation provides model recommendations based on hardware
// profile, benchmark results, and model metadata.
package recommendation

import (
	"errors"
	"fmt"
	"sort"

	"apcode/internal/benchmark"
	"apcode/internal/hardware"
	"apcode/internal/model"
)

// ErrNoCandidates is returned when no models are compatible.
var ErrNoCandidates = errors.New("recommendation: no compatible models")

// ErrNoModels is returned when the registry is empty.
var ErrNoModels = errors.New("recommendation: no models in registry")

// ErrNoCapabilityMatch is returned when no models support the requested capability.
var ErrNoCapabilityMatch = errors.New("recommendation: no models support requested capability")

// Preference weights for scoring.
const (
	WeightCapabilityMatch = 30 // Capability match
	WeightMemoryFit       = 25 // Memory fit (minimum vs available)
	WeightModelEfficiency = 15 // Model size efficiency
	WeightBenchmarkSuit   = 10 // Benchmark suitability
	WeightContextLength   = 10 // Context length
	WeightInstalledBonus  = 10 // Installed model bonus
	MaxFitScore           = 100
)

// PreferenceMode indicates user preference for ranking.
type PreferenceMode int

const (
	// PreferenceBalanced balances speed, quality, and efficiency.
	PreferenceBalanced PreferenceMode = iota
	// PreferenceSpeed prioritizes smaller, faster models.
	PreferenceSpeed
	// PreferenceQuality prioritizes larger, higher-quality models.
	PreferenceQuality
	// PreferenceMemory prioritizes memory-efficient models.
	PreferenceMemory
	// PreferenceContext prioritizes longer context windows.
	PreferenceContext
)

// RecommendationInput contains all data needed for recommendation.
type RecommendationInput struct {
	// Hardware is the detected hardware profile.
	Hardware hardware.HardwareProfile

	// Benchmark is the benchmark result (may be nil if not run).
	Benchmark *benchmark.Result

	// Models is the list of available models to evaluate.
	Models []*model.ModelMetadata

	// RequestedCapability is the capability the user needs.
	// If empty, all capabilities are considered.
	RequestedCapability model.Capability

	// Preference is the user's ranking preference.
	Preference PreferenceMode
}

// Validate checks the input for validity.
func (i *RecommendationInput) Validate() error {
	// Cloud-only models don't require hardware profile.
	allCloud := true
	for _, m := range i.Models {
		if m.Source != model.SourceFreeCloud && m.Source != model.SourcePaidCloud {
			allCloud = false
			break
		}
	}
	if !allCloud && i.Hardware.OS == "" && i.Hardware.Arch == "" {
		return errors.New("recommendation: hardware profile is empty")
	}
	if len(i.Models) == 0 {
		return ErrNoModels
	}
	return nil
}

// RAMStatus indicates the RAM compatibility level.
type RAMStatus int

const (
	// RAMStatusIncompatible means minimum RAM exceeds available.
	RAMStatusIncompatible RAMStatus = iota
	// RAMStatusTight means minimum RAM fits but recommended exceeds available.
	RAMStatusTight
	// RAMStatusGood means recommended RAM fits in available.
	RAMStatusGood
)

// MemoryFit contains memory compatibility details.
type MemoryFit struct {
	Status         RAMStatus
	AvailableRAM   uint64
	MinimumRAM     uint64
	RecommendedRAM uint64
	AvailableKnown bool
	Reason         string
}

// EvaluateMemoryFit evaluates a model's RAM compatibility.
func EvaluateMemoryFit(profile hardware.HardwareProfile, m *model.ModelMetadata) MemoryFit {
	availableRAM := profile.AvailableRAMBytes
	availableKnown := profile.AvailableRAMKnown

	// If available RAM unknown, use total RAM as conservative estimate
	if !availableKnown || availableRAM == 0 {
		availableRAM = profile.TotalRAMBytes
		availableKnown = false
	}

	if m.MinimumRAMBytes > availableRAM {
		return MemoryFit{
			Status:         RAMStatusIncompatible,
			AvailableRAM:   availableRAM,
			MinimumRAM:     m.MinimumRAMBytes,
			RecommendedRAM: m.RecommendedRAMBytes,
			AvailableKnown: availableKnown,
			Reason:         fmt.Sprintf("minimum RAM (%.1f GiB) exceeds available (%.1f GiB)", float64(m.MinimumRAMBytes)/1024/1024/1024, float64(availableRAM)/1024/1024/1024),
		}
	}

	if m.RecommendedRAMBytes > availableRAM {
		return MemoryFit{
			Status:         RAMStatusTight,
			AvailableRAM:   availableRAM,
			MinimumRAM:     m.MinimumRAMBytes,
			RecommendedRAM: m.RecommendedRAMBytes,
			AvailableKnown: availableKnown,
			Reason:         fmt.Sprintf("recommended RAM (%.1f GiB) exceeds available (%.1f GiB); minimum (%.1f GiB) satisfied", float64(m.RecommendedRAMBytes)/1024/1024/1024, float64(availableRAM)/1024/1024/1024, float64(m.MinimumRAMBytes)/1024/1024/1024),
		}
	}

	return MemoryFit{
		Status:         RAMStatusGood,
		AvailableRAM:   availableRAM,
		MinimumRAM:     m.MinimumRAMBytes,
		RecommendedRAM: m.RecommendedRAMBytes,
		AvailableKnown: availableKnown,
		Reason:         fmt.Sprintf("recommended RAM (%.1f GiB) fits in available (%.1f GiB)", float64(m.RecommendedRAMBytes)/1024/1024/1024, float64(availableRAM)/1024/1024/1024),
	}
}

// Candidate represents an evaluated model candidate.
type Candidate struct {
	Model           *model.ModelMetadata
	FitScore        int
	MemoryFit       MemoryFit
	CapabilityMatch bool
	Reasons         []string
	Warnings        []string
	Rejected        bool
	RejectionReason string
}

// RecommendationResult contains the recommendation outcome.
type RecommendationResult struct {
	Input             RecommendationInput
	Recommended       *Candidate
	Candidates        []*Candidate
	Rejected          []*Candidate
	Uncertainty       string
	BenchmarkRun      bool
	AvailableRAMKnown bool
}

// Recommender performs model recommendations.
type Recommender struct{}

// NewRecommender creates a new recommender.
func NewRecommender() *Recommender {
	return &Recommender{}
}

// Recommend evaluates models and returns a recommendation.
func (r *Recommender) Recommend(input RecommendationInput) (RecommendationResult, error) {
	if err := input.Validate(); err != nil {
		return RecommendationResult{}, err
	}

	result := RecommendationResult{
		Input:             input,
		BenchmarkRun:      input.Benchmark != nil,
		AvailableRAMKnown: input.Hardware.AvailableRAMKnown && input.Hardware.AvailableRAMBytes > 0,
	}

	// Filter by capability if requested
	candidates := input.Models
	if input.RequestedCapability != "" {
		var filtered []*model.ModelMetadata
		for _, m := range input.Models {
			if m.Capabilities.Has(input.RequestedCapability) {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 0 {
			return RecommendationResult{}, ErrNoCapabilityMatch
		}
		candidates = filtered
	}

	// Evaluate each candidate
	var evaluated []*Candidate
	for _, m := range candidates {
		c := r.evaluateCandidate(input, m)
		evaluated = append(evaluated, c)
	}

	// Separate rejected and valid candidates
	var valid []*Candidate
	for _, c := range evaluated {
		if c.Rejected {
			result.Rejected = append(result.Rejected, c)
		} else {
			valid = append(valid, c)
		}
	}

	if len(valid) == 0 {
		return RecommendationResult{}, ErrNoCandidates
	}

	// Sort by fit score descending
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].FitScore != valid[j].FitScore {
			return valid[i].FitScore > valid[j].FitScore
		}
		// Tie-breaker: prefer installed models
		if valid[i].Model.Installed != valid[j].Model.Installed {
			return valid[i].Model.Installed
		}
		// Tie-breaker: smaller model
		if valid[i].Model.FileSizeBytes != valid[j].Model.FileSizeBytes {
			return valid[i].Model.FileSizeBytes < valid[j].Model.FileSizeBytes
		}
		// Final tie-breaker: lexicographic ID for determinism
		return valid[i].Model.ID < valid[j].Model.ID
	})

	result.Candidates = valid
	result.Recommended = valid[0]

	// Build uncertainty message
	result.Uncertainty = r.buildUncertainty(input, result)

	return result, nil
}

func (r *Recommender) evaluateCandidate(input RecommendationInput, m *model.ModelMetadata) *Candidate {
	c := &Candidate{
		Model:    m,
		FitScore: 0,
		Reasons:  []string{},
		Warnings: []string{},
		Rejected: false,
	}

	// Cloud models are not constrained by local hardware RAM.
	if m.Source == model.SourceFreeCloud || m.Source == model.SourcePaidCloud {
		// 1. Capability match for cloud models
		c.CapabilityMatch = m.Capabilities.Has(input.RequestedCapability)
		if input.RequestedCapability != "" && c.CapabilityMatch {
			c.FitScore += WeightCapabilityMatch
			c.Reasons = append(c.Reasons, fmt.Sprintf("Supports %s", input.RequestedCapability))
		} else if input.RequestedCapability != "" {
			c.FitScore += 0
		} else {
			c.FitScore += WeightCapabilityMatch / 2
			c.Reasons = append(c.Reasons, "General purpose model")
		}
		// Cloud models don't need hardware RAM checks.
		c.MemoryFit = MemoryFit{Status: RAMStatusGood, Reason: "Cloud model; no local RAM constraint"}
		// 2. Model efficiency (smaller models score higher for efficiency)
		efficiencyScore := r.scoreEfficiency(m, input.Preference)
		c.FitScore += efficiencyScore
		// 3. Context length
		contextScore := r.scoreContextLength(m, input.Preference)
		c.FitScore += contextScore
		c.FitScore += WeightCapabilityMatch / 2 // Cloud availability bonus
		// Cap at max
		if c.FitScore > MaxFitScore {
			c.FitScore = MaxFitScore
		}
		return c
	}

	// 1. Evaluate memory fit (HARD CONSTRAINT) for local models.
	memFit := EvaluateMemoryFit(input.Hardware, m)
	c.MemoryFit = memFit

	if memFit.Status == RAMStatusIncompatible {
		c.Rejected = true
		c.RejectionReason = memFit.Reason
		c.Warnings = append(c.Warnings, "HARD: "+memFit.Reason)
		return c
	}

	// 2. Capability match
	c.CapabilityMatch = m.Capabilities.Has(input.RequestedCapability)
	if input.RequestedCapability != "" && c.CapabilityMatch {
		c.FitScore += WeightCapabilityMatch
		c.Reasons = append(c.Reasons, fmt.Sprintf("Supports %s", input.RequestedCapability))
	} else if input.RequestedCapability != "" {
		// Shouldn't happen due to pre-filtering, but handle anyway
		c.FitScore += 0
	} else {
		c.FitScore += WeightCapabilityMatch / 2 // Partial credit for general capability
		c.Reasons = append(c.Reasons, "General purpose model")
	}

	// 3. Memory fit scoring (SOFT)
	switch memFit.Status {
	case RAMStatusGood:
		c.FitScore += WeightMemoryFit
		c.Reasons = append(c.Reasons, "Recommended RAM fits available memory")
	case RAMStatusTight:
		c.FitScore += WeightMemoryFit / 2
		c.Warnings = append(c.Warnings, memFit.Reason)
		c.Reasons = append(c.Reasons, "Minimum RAM satisfied; recommended RAM exceeds available")
	}

	// 4. Model efficiency (smaller models score higher for efficiency)
	efficiencyScore := r.scoreEfficiency(m, input.Preference)
	c.FitScore += efficiencyScore
	if efficiencyScore > WeightModelEfficiency/2 {
		c.Reasons = append(c.Reasons, fmt.Sprintf("Efficient model size (%.1f GiB)", float64(m.FileSizeBytes)/1024/1024/1024))
	}

	// 5. Benchmark suitability
	benchScore := r.scoreBenchmark(input, m)
	c.FitScore += benchScore
	if benchScore > 0 {
		c.Reasons = append(c.Reasons, "Suitable for measured hardware performance")
	}

	// 6. Context length
	contextScore := r.scoreContextLength(m, input.Preference)
	c.FitScore += contextScore
	if contextScore > WeightContextLength/2 {
		c.Reasons = append(c.Reasons, fmt.Sprintf("Good context length (%s)", formatContext(m.ContextLength)))
	}

	// 7. Installed bonus
	if m.Installed {
		c.FitScore += WeightInstalledBonus
		c.Reasons = append(c.Reasons, "Already installed locally")
	}

	// Cap at max
	if c.FitScore > MaxFitScore {
		c.FitScore = MaxFitScore
	}

	return c
}

func (r *Recommender) scoreEfficiency(m *model.ModelMetadata, pref PreferenceMode) int {
	// Score based on model size - smaller is more efficient
	// Normalize: 1-2B = high, 3-7B = medium, 8-13B = low, 14B+ = very low
	sizeGB := float64(m.FileSizeBytes) / 1024 / 1024 / 1024

	var baseScore int
	switch {
	case sizeGB <= 2:
		baseScore = WeightModelEfficiency
	case sizeGB <= 4:
		baseScore = int(float64(WeightModelEfficiency) * 0.8)
	case sizeGB <= 8:
		baseScore = int(float64(WeightModelEfficiency) * 0.6)
	case sizeGB <= 12:
		baseScore = int(float64(WeightModelEfficiency) * 0.4)
	default:
		baseScore = int(float64(WeightModelEfficiency) * 0.2)
	}

	// Adjust for preference
	switch pref {
	case PreferenceSpeed:
		// Prefer smaller models
		return baseScore
	case PreferenceQuality:
		// Prefer larger models
		return WeightModelEfficiency - baseScore
	case PreferenceMemory:
		// Strongly prefer smaller
		return baseScore + WeightModelEfficiency/4
	case PreferenceContext:
		// Neutral on size
		return baseScore
	default:
		// Balanced
		return baseScore
	}
}

func (r *Recommender) scoreBenchmark(input RecommendationInput, m *model.ModelMetadata) int {
	if input.Benchmark == nil {
		return 0 // No benchmark data, neutral
	}

	// Use CPU and memory benchmark to influence
	// Higher CPU ops/sec -> can handle larger models better
	// Higher memory bandwidth -> can handle larger models better

	cpuOpsPerSec := input.Benchmark.CPU.OperationsPerSec
	memBytesPerSec := input.Benchmark.Memory.BytesPerSec

	// Normalize benchmarks (these are arbitrary baselines)
	// CPU: ~10M ops/sec is baseline
	// Memory: ~10 GiB/sec is baseline
	cpuRatio := cpuOpsPerSec / 10_000_000
	memRatio := memBytesPerSec / (10 * 1024 * 1024 * 1024)

	// If hardware is fast, slight bonus for larger models
	// If hardware is slow, slight bonus for smaller models
	sizeGB := float64(m.FileSizeBytes) / 1024 / 1024 / 1024

	var score int
	if cpuRatio > 1.5 && memRatio > 1.5 {
		// Fast hardware - slight preference for larger models
		if sizeGB > 6 {
			score = WeightBenchmarkSuit / 2
		}
	} else if cpuRatio < 0.5 || memRatio < 0.5 {
		// Slow hardware - prefer smaller models
		if sizeGB <= 4 {
			score = WeightBenchmarkSuit / 2
		}
	}

	return score
}

func (r *Recommender) scoreContextLength(m *model.ModelMetadata, pref PreferenceMode) int {
	// Score based on context length
	var score int
	switch {
	case m.ContextLength >= 100000:
		score = WeightContextLength
	case m.ContextLength >= 32000:
		score = int(float64(WeightContextLength) * 0.8)
	case m.ContextLength >= 16000:
		score = int(float64(WeightContextLength) * 0.6)
	case m.ContextLength >= 8000:
		score = int(float64(WeightContextLength) * 0.4)
	default:
		score = int(float64(WeightContextLength) * 0.2)
	}

	// Adjust for preference
	if pref == PreferenceContext {
		return score + WeightContextLength/4
	}
	return score
}

func (r *Recommender) buildUncertainty(input RecommendationInput, result RecommendationResult) string {
	var parts []string

	if !input.Hardware.AvailableRAMKnown || input.Hardware.AvailableRAMBytes == 0 {
		parts = append(parts, "Available RAM unknown; used total RAM as estimate")
	}

	if !result.BenchmarkRun {
		parts = append(parts, "No benchmark data; CPU/memory performance not measured")
	}

	if input.RequestedCapability == "" {
		parts = append(parts, "No specific capability requested; general recommendation")
	}

	if len(parts) == 0 {
		return "No significant uncertainties"
	}

	return "Uncertainties: " + joinWithSemicolon(parts)
}

// RecommendFreeCloudModels evaluates free cloud models without hardware constraints.
// This is separate from Recommend because cloud models do not have local RAM requirements.
func (r *Recommender) RecommendFreeCloudModels(models []*model.ModelMetadata) ([]*Candidate, error) {
	if len(models) == 0 {
		return nil, ErrNoModels
	}
	var candidates []*Candidate
	for _, m := range models {
		if m.Source != model.SourceFreeCloud {
			continue
		}
		c := &Candidate{
			Model:    m,
			FitScore: 0,
			Reasons:  []string{},
			Warnings: []string{},
			Rejected: false,
		}
		c.MemoryFit = MemoryFit{Status: RAMStatusGood, Reason: "Cloud model; no local RAM constraint"}
		// Score by context length and capabilities
		contextScore := r.scoreContextLength(m, PreferenceBalanced)
		c.FitScore += contextScore
		c.FitScore += WeightCapabilityMatch / 2 // Cloud availability bonus
		if c.FitScore > MaxFitScore {
			c.FitScore = MaxFitScore
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return nil, ErrNoCandidates
	}
	// Sort by fit score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].FitScore > candidates[j].FitScore
	})
	return candidates, nil
}

func formatContext(tokens int) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%.1fM tokens", float64(tokens)/1000000)
	}
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fK tokens", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d tokens", tokens)
}

func joinWithSemicolon(items []string) string {
	if len(items) == 0 {
		return ""
	}
	result := items[0]
	for _, item := range items[1:] {
		result += "; " + item
	}
	return result
}
