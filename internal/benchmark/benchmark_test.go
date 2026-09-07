package benchmark

import (
	"context"
	"testing"
	"time"

	"apcode/internal/hardware"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.CPUWorkUnits <= 0 {
		t.Error("CPUWorkUnits should be positive")
	}
	if config.MemorySizeBytes == 0 {
		t.Error("MemorySizeBytes should be positive")
	}
	if config.MemoryPasses <= 0 {
		t.Error("MemoryPasses should be positive")
	}
	if config.Timeout <= 0 {
		t.Error("Timeout should be positive")
	}
	if !config.CPUEnabled {
		t.Error("CPUEnabled should be true by default")
	}
	if !config.MemoryEnabled {
		t.Error("MemoryEnabled should be true by default")
	}
	if config.StorageEnabled {
		t.Error("StorageEnabled should be false by default")
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "zero CPU work units",
			config: Config{
				CPUWorkUnits:     0,
				MemorySizeBytes:  64 * 1024 * 1024,
				MemoryPasses:     3,
				StorageEnabled:   false,
				StorageSizeBytes: 256 * 1024 * 1024,
				Timeout:          60 * time.Second,
				CPUEnabled:       true,
				MemoryEnabled:    true,
			},
			wantErr: true,
		},
		{
			name: "zero memory size",
			config: Config{
				CPUWorkUnits:     100000,
				MemorySizeBytes:  0,
				MemoryPasses:     3,
				StorageEnabled:   false,
				StorageSizeBytes: 256 * 1024 * 1024,
				Timeout:          60 * time.Second,
				CPUEnabled:       true,
				MemoryEnabled:    true,
			},
			wantErr: true,
		},
		{
			name: "zero memory passes",
			config: Config{
				CPUWorkUnits:     100000,
				MemorySizeBytes:  64 * 1024 * 1024,
				MemoryPasses:     0,
				StorageEnabled:   false,
				StorageSizeBytes: 256 * 1024 * 1024,
				Timeout:          60 * time.Second,
				CPUEnabled:       true,
				MemoryEnabled:    true,
			},
			wantErr: true,
		},
		{
			name: "storage enabled but zero size",
			config: Config{
				CPUWorkUnits:     100000,
				MemorySizeBytes:  64 * 1024 * 1024,
				MemoryPasses:     3,
				StorageEnabled:   true,
				StorageSizeBytes: 0,
				Timeout:          60 * time.Second,
				CPUEnabled:       true,
				MemoryEnabled:    true,
			},
			wantErr: true,
		},
		{
			name: "zero timeout",
			config: Config{
				CPUWorkUnits:     100000,
				MemorySizeBytes:  64 * 1024 * 1024,
				MemoryPasses:     3,
				StorageEnabled:   false,
				StorageSizeBytes: 256 * 1024 * 1024,
				Timeout:          0,
				CPUEnabled:       true,
				MemoryEnabled:    true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunCPUBenchmark(t *testing.T) {
	ctx := context.Background()
	config := Config{
		CPUWorkers:   2,
		CPUWorkUnits: 500000, // Enough work that duration is reliably positive even on VMs with coarse clocks
		Timeout:      10 * time.Second,
	}

	result := RunCPUBenchmark(ctx, config, 4)

	if !result.Success {
		t.Errorf("CPU benchmark failed: %v", result.Error)
	}
	if result.Operations <= 0 {
		t.Error("Operations should be positive")
	}
	if result.Workers != 2 {
		t.Errorf("Expected 2 workers, got %d", result.Workers)
	}
	if result.Duration <= 0 {
		t.Error("Duration should be positive")
	}
	if result.OperationsPerSec <= 0 {
		t.Error("OperationsPerSec should be positive")
	}
}

func TestRunCPUBenchmarkWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := Config{
		CPUWorkers:   2,
		CPUWorkUnits: 1000000, // Large to allow cancellation
		Timeout:      10 * time.Second,
	}

	// Cancel immediately
	cancel()

	result := RunCPUBenchmark(ctx, config, 4)

	if result.Success {
		t.Error("Expected benchmark to be cancelled")
	}
	if result.Error == nil {
		t.Error("Expected error from context cancellation")
	}
}

func TestRunMemoryBenchmark(t *testing.T) {
	ctx := context.Background()
	config := Config{
		MemorySizeBytes: 16 * 1024 * 1024, // 16 MiB for more reliable timing
		MemoryPasses:    2,
		Timeout:         10 * time.Second,
	}

	result := RunMemoryBenchmark(ctx, config)

	if !result.Success {
		t.Errorf("Memory benchmark failed: %v", result.Error)
	}
	if result.BytesProcessed == 0 {
		t.Error("BytesProcessed should be positive")
	}
	if result.Passes != 2 {
		t.Errorf("Expected 2 passes, got %d", result.Passes)
	}
	if result.Duration <= 0 {
		t.Error("Duration should be positive")
	}
	if result.BytesPerSec <= 0 {
		t.Error("BytesPerSec should be positive")
	}
}

func TestRunMemoryBenchmarkWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := Config{
		MemorySizeBytes: 100 * 1024 * 1024, // 100 MiB to allow cancellation
		MemoryPasses:    10,
		Timeout:         10 * time.Second,
	}

	// Cancel immediately
	cancel()

	result := RunMemoryBenchmark(ctx, config)

	if result.Success {
		t.Error("Expected benchmark to be cancelled")
	}
	if result.Error == nil {
		t.Error("Expected error from context cancellation")
	}
}

func TestRunMemoryBenchmarkRespectsSizeLimit(t *testing.T) {
	ctx := context.Background()
	config := Config{
		MemorySizeBytes: 1024 * 1024 * 1024, // 1 GiB - should be capped
		MemoryPasses:    1,
		Timeout:         10 * time.Second,
	}

	result := RunMemoryBenchmark(ctx, config)

	if !result.Success {
		t.Errorf("Memory benchmark failed: %v", result.Error)
	}
	// Should be capped at 512 MiB
	if result.BytesProcessed > uint64(512*1024*1024*2) { // *2 for read+write
		t.Errorf("Memory benchmark processed too much data: %d bytes", result.BytesProcessed)
	}
}

func TestRunStorageBenchmark(t *testing.T) {
	ctx := context.Background()
	config := Config{
		StorageEnabled:   true,
		StorageSizeBytes: 1024 * 1024, // 1 MiB for fast test
		Timeout:          10 * time.Second,
	}

	result := RunStorageBenchmark(ctx, config)

	if !result.Success {
		t.Errorf("Storage benchmark failed: %v", result.Error)
	}
	if result.WriteBytes == 0 {
		t.Error("WriteBytes should be positive")
	}
	if result.ReadBytes == 0 {
		t.Error("ReadBytes should be positive")
	}
	if result.WriteDuration <= 0 {
		t.Error("WriteDuration should be positive")
	}
	if result.ReadDuration <= 0 {
		t.Error("ReadDuration should be positive")
	}
	if result.WriteBytesPerSec <= 0 {
		t.Error("WriteBytesPerSec should be positive")
	}
	if result.ReadBytesPerSec <= 0 {
		t.Error("ReadBytesPerSec should be positive")
	}
}

func TestRunStorageBenchmarkDisabled(t *testing.T) {
	ctx := context.Background()
	config := Config{
		StorageEnabled:   false,
		StorageSizeBytes: 256 * 1024 * 1024,
		Timeout:          10 * time.Second,
	}

	result := RunStorageBenchmark(ctx, config)

	if result.Success {
		t.Error("Expected storage benchmark to be disabled")
	}
	if result.Error != ErrStorageDisabled {
		t.Errorf("Expected ErrStorageDisabled, got %v", result.Error)
	}
}

func TestRunStorageBenchmarkWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	config := Config{
		StorageEnabled:   true,
		StorageSizeBytes: 100 * 1024 * 1024, // 100 MiB to allow cancellation
		Timeout:          10 * time.Second,
	}

	// Cancel immediately
	cancel()

	result := RunStorageBenchmark(ctx, config)

	if result.Success {
		t.Error("Expected benchmark to be cancelled")
	}
	if result.Error == nil {
		t.Error("Expected error from context cancellation")
	}
}

func TestRunnerRun(t *testing.T) {
	ctx := context.Background()
	profile := hardware.HardwareProfile{
		LogicalCPUs: 4,
	}
	config := Config{
		CPUWorkers:      1,
		CPUWorkUnits:    100, // Very small for fast test
		MemorySizeBytes: 1024 * 1024,
		MemoryPasses:    1,
		StorageEnabled:  false,
		Timeout:         10 * time.Second,
		CPUEnabled:      true,
		MemoryEnabled:   true,
	}

	runner := &BenchmarkRunner{}
	result, err := runner.Run(ctx, profile, config)

	if err != nil {
		t.Fatalf("Runner.Run failed: %v", err)
	}

	if !result.CPU.Success {
		t.Errorf("CPU benchmark failed: %v", result.CPU.Error)
	}
	if !result.Memory.Success {
		t.Errorf("Memory benchmark failed: %v", result.Memory.Error)
	}
	if result.Storage.Success {
		t.Error("Storage benchmark should be disabled")
	}
	if result.Version == "" {
		t.Error("Version should be set")
	}
	if result.Duration <= 0 {
		t.Error("Duration should be positive")
	}
}

func TestRunnerRunInvalidConfig(t *testing.T) {
	ctx := context.Background()
	profile := hardware.HardwareProfile{
		LogicalCPUs: 4,
	}
	config := Config{
		CPUWorkUnits: 0, // Invalid
	}

	runner := &BenchmarkRunner{}
	_, err := runner.Run(ctx, profile, config)

	if err == nil {
		t.Error("Expected error for invalid config")
	}
}
