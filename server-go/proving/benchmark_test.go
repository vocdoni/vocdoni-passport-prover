package proving

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// BenchmarkConfig holds configuration for the benchmark
type BenchmarkConfig struct {
	BBBinaryPath  string
	ArtifactsDir  string
	WorkspaceRoot string
	CRSPath       string
}

// CPUInfo contains information about the CPU
type CPUInfo struct {
	ModelName    string   `json:"model_name"`
	Architecture string   `json:"architecture"`
	NumCPUs      int      `json:"num_cpus"`
	Flags        []string `json:"flags,omitempty"`
}

// BenchmarkResult holds the results of a benchmark run
type BenchmarkResult struct {
	TestName       string        `json:"test_name"`
	Duration       time.Duration `json:"duration_ns"`
	DurationHuman  string        `json:"duration_human"`
	CPUInfo        CPUInfo       `json:"cpu_info"`
	BBVersion      string        `json:"bb_version,omitempty"`
	BBBuildInfo    string        `json:"bb_build_info,omitempty"`
	Error          string        `json:"error,omitempty"`
	BenchmarkStats interface{}   `json:"benchmark_stats,omitempty"`
}

func getBBBinaryPath() string {
	if path := os.Getenv("BB_BINARY_PATH"); path != "" {
		return path
	}
	if path, err := exec.LookPath("bb"); err == nil {
		return path
	}
	return ""
}

func getCPUInfo() CPUInfo {
	info := CPUInfo{
		Architecture: runtime.GOARCH,
		NumCPUs:      runtime.NumCPU(),
	}

	// Try to get CPU model name from /proc/cpuinfo on Linux
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "model name") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						info.ModelName = strings.TrimSpace(parts[1])
					}
				}
				if strings.HasPrefix(line, "flags") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						flags := strings.Fields(strings.TrimSpace(parts[1]))
						// Filter for relevant SIMD flags
						relevantFlags := []string{}
						simdFlags := map[string]bool{
							"sse": true, "sse2": true, "sse3": true, "ssse3": true,
							"sse4_1": true, "sse4_2": true, "avx": true, "avx2": true,
							"avx512f": true, "avx512dq": true, "avx512cd": true,
							"avx512bw": true, "avx512vl": true, "avx512_vnni": true,
							"fma": true, "bmi1": true, "bmi2": true, "adx": true,
						}
						for _, flag := range flags {
							if simdFlags[flag] {
								relevantFlags = append(relevantFlags, flag)
							}
						}
						info.Flags = relevantFlags
					}
					break
				}
			}
		}
	}

	return info
}

func getBBVersion(bbPath string) (version, buildInfo string) {
	cmd := exec.Command(bbPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown", ""
	}
	version = strings.TrimSpace(string(output))

	// Try to get more build info
	cmd = exec.Command("file", bbPath)
	output, err = cmd.Output()
	if err == nil {
		buildInfo = strings.TrimSpace(string(output))
	}

	return version, buildInfo
}

// checkBBSIMDSupport checks what SIMD instructions the bb binary uses
func checkBBSIMDSupport(bbPath string) map[string]int {
	results := make(map[string]int)

	// Use objdump to check for SIMD instructions
	cmd := exec.Command("objdump", "-d", bbPath)
	output, err := cmd.Output()
	if err != nil {
		return results
	}

	content := string(output)

	// Count different instruction types
	patterns := map[string][]string{
		"avx512": {"zmm", "vpbroadcast", "vpmull"},
		"avx2":   {"ymm"},
		"avx":    {"vmov", "vpadd", "vmul"},
		"sse":    {"xmm"},
	}

	for name, keywords := range patterns {
		count := 0
		for _, kw := range keywords {
			count += strings.Count(content, kw)
		}
		results[name] = count
	}

	return results
}

// TestBBBinaryInfo prints information about the bb binary
func TestBBBinaryInfo(t *testing.T) {
	bbPath := getBBBinaryPath()
	if bbPath == "" {
		t.Skip("bb binary not found, skipping")
	}

	t.Logf("BB Binary Path: %s", bbPath)

	version, buildInfo := getBBVersion(bbPath)
	t.Logf("BB Version: %s", version)
	t.Logf("BB Build Info: %s", buildInfo)

	cpuInfo := getCPUInfo()
	t.Logf("CPU Model: %s", cpuInfo.ModelName)
	t.Logf("Architecture: %s", cpuInfo.Architecture)
	t.Logf("Num CPUs: %d", cpuInfo.NumCPUs)
	t.Logf("SIMD Flags: %v", cpuInfo.Flags)

	// Check SIMD support in binary
	simdSupport := checkBBSIMDSupport(bbPath)
	t.Logf("BB SIMD instruction counts:")
	for name, count := range simdSupport {
		t.Logf("  %s: %d", name, count)
	}
}

// TestBBHelp verifies bb is working and shows available options
func TestBBHelp(t *testing.T) {
	bbPath := getBBBinaryPath()
	if bbPath == "" {
		t.Skip("bb binary not found, skipping")
	}

	cmd := exec.Command(bbPath, "prove", "--help-extended")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bb prove --help-extended failed: %v\n%s", err, output)
	}

	// Check for benchmark options
	outputStr := string(output)
	benchmarkOptions := []string{"--print_bench", "--bench_out", "--bench_out_hierarchical"}
	for _, opt := range benchmarkOptions {
		if strings.Contains(outputStr, opt) {
			t.Logf("Found benchmark option: %s", opt)
		} else {
			t.Logf("Missing benchmark option: %s", opt)
		}
	}
}

// BenchmarkServiceConfig tests the service configuration
func TestServiceConfig(t *testing.T) {
	logger := zerolog.Nop()

	testCases := []struct {
		name           string
		maxConcurrency int
		lowMemoryMode  bool
		timeout        time.Duration
	}{
		{"default", 1, false, 5 * time.Minute},
		{"high_concurrency", 4, false, 5 * time.Minute},
		{"low_memory", 1, true, 10 * time.Minute},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				MaxConcurrency: tc.maxConcurrency,
				LowMemoryMode:  tc.lowMemoryMode,
				Timeout:        tc.timeout,
			}
			svc := NewService(cfg, logger)
			if svc == nil {
				t.Fatal("NewService returned nil")
			}
			t.Logf("Service created with config: maxConcurrency=%d, lowMemoryMode=%v, timeout=%v",
				tc.maxConcurrency, tc.lowMemoryMode, tc.timeout)
		})
	}
}

// TestEnvironmentVariables checks that all required environment variables are set
func TestEnvironmentVariables(t *testing.T) {
	envVars := []struct {
		name     string
		required bool
	}{
		{"BB_BINARY_PATH", false},
		{"CRS_PATH", false},
		{"VOCDONI_WORKSPACE_ROOT", false},
		{"VOCDONI_ARTIFACTS_DIR", false},
		{"VOCDONI_PROVER_BINARY_PATH", false},
	}

	for _, ev := range envVars {
		value := os.Getenv(ev.name)
		if value == "" {
			if ev.required {
				t.Errorf("Required environment variable %s is not set", ev.name)
			} else {
				t.Logf("Optional environment variable %s is not set", ev.name)
			}
		} else {
			t.Logf("%s = %s", ev.name, value)
		}
	}
}

// BenchmarkReport generates a comprehensive benchmark report
func TestGenerateBenchmarkReport(t *testing.T) {
	bbPath := getBBBinaryPath()

	report := struct {
		Timestamp   string                 `json:"timestamp"`
		CPUInfo     CPUInfo                `json:"cpu_info"`
		BBInfo      map[string]interface{} `json:"bb_info"`
		Environment map[string]string      `json:"environment"`
		SIMDSupport map[string]int         `json:"simd_support"`
	}{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		CPUInfo:     getCPUInfo(),
		BBInfo:      make(map[string]interface{}),
		Environment: make(map[string]string),
		SIMDSupport: make(map[string]int),
	}

	// Collect BB info
	if bbPath != "" {
		version, buildInfo := getBBVersion(bbPath)
		report.BBInfo["path"] = bbPath
		report.BBInfo["version"] = version
		report.BBInfo["build_info"] = buildInfo
		report.SIMDSupport = checkBBSIMDSupport(bbPath)
	} else {
		report.BBInfo["error"] = "bb binary not found"
	}

	// Collect environment variables
	envVars := []string{
		"BB_BINARY_PATH", "CRS_PATH", "VOCDONI_WORKSPACE_ROOT",
		"VOCDONI_ARTIFACTS_DIR", "VOCDONI_PROVER_BINARY_PATH",
	}
	for _, ev := range envVars {
		if value := os.Getenv(ev); value != "" {
			report.Environment[ev] = value
		}
	}

	// Output report
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal report: %v", err)
	}

	t.Logf("Benchmark Report:\n%s", string(reportJSON))

	// Optionally write to file
	reportPath := os.Getenv("BENCHMARK_REPORT_PATH")
	if reportPath != "" {
		if err := os.WriteFile(reportPath, reportJSON, 0644); err != nil {
			t.Errorf("Failed to write report to %s: %v", reportPath, err)
		} else {
			t.Logf("Report written to %s", reportPath)
		}
	}
}

// BenchmarkProverCLI benchmarks the prover-cli aggregate-request command
func BenchmarkProverCLI(b *testing.B) {
	proverPath := os.Getenv("VOCDONI_PROVER_BINARY_PATH")
	if proverPath == "" {
		b.Skip("VOCDONI_PROVER_BINARY_PATH not set, skipping benchmark")
	}

	artifactsDir := os.Getenv("VOCDONI_ARTIFACTS_DIR")
	if artifactsDir == "" {
		b.Skip("VOCDONI_ARTIFACTS_DIR not set, skipping benchmark")
	}

	// Check if we have a test request file
	testRequestPath := os.Getenv("BENCHMARK_REQUEST_PATH")
	if testRequestPath == "" {
		b.Skip("BENCHMARK_REQUEST_PATH not set, skipping benchmark")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

		outputFile, err := os.CreateTemp("", "benchmark-output-*.json")
		if err != nil {
			b.Fatalf("Failed to create temp file: %v", err)
		}
		outputPath := outputFile.Name()
		outputFile.Close()

		cmd := exec.CommandContext(ctx,
			proverPath,
			"aggregate-request",
			"--input", testRequestPath,
			"--artifacts-dir", artifactsDir,
			"--output", outputPath,
		)

		output, err := cmd.CombinedOutput()
		cancel()
		os.Remove(outputPath)

		if err != nil {
			b.Fatalf("prover-cli failed: %v\n%s", err, output)
		}
	}
}

// TestCPUOptimizationRecommendations provides recommendations based on the system
func TestCPUOptimizationRecommendations(t *testing.T) {
	cpuInfo := getCPUInfo()
	bbPath := getBBBinaryPath()

	t.Log("=== CPU Optimization Recommendations ===")
	t.Log("")

	// Check for AVX-512 support
	hasAVX512 := false
	hasAVX2 := false
	for _, flag := range cpuInfo.Flags {
		if strings.HasPrefix(flag, "avx512") {
			hasAVX512 = true
		}
		if flag == "avx2" {
			hasAVX2 = true
		}
	}

	if hasAVX512 {
		t.Log("✓ CPU supports AVX-512 instructions")
		t.Log("  Recommendation: Ensure bb is compiled with -march=native or a specific")
		t.Log("  AVX-512 supporting architecture (e.g., -march=skylake-avx512, -march=znver4)")
	} else if hasAVX2 {
		t.Log("✓ CPU supports AVX2 instructions")
		t.Log("  Recommendation: Ensure bb is compiled with -march=native or -march=haswell")
	} else {
		t.Log("⚠ CPU may not support modern SIMD instructions")
	}

	t.Log("")

	// Check bb binary SIMD usage
	if bbPath != "" {
		simdSupport := checkBBSIMDSupport(bbPath)
		if simdSupport["avx512"] > 0 {
			t.Logf("✓ bb binary uses AVX-512 instructions (%d occurrences)", simdSupport["avx512"])
		} else if hasAVX512 {
			t.Log("⚠ bb binary does NOT use AVX-512 instructions, but CPU supports it")
			t.Log("  Recommendation: Rebuild bb with -march=native on this machine")
		}

		if simdSupport["avx2"] > 0 {
			t.Logf("✓ bb binary uses AVX2 instructions (%d occurrences)", simdSupport["avx2"])
		}
	}

	t.Log("")
	t.Log("=== Docker Build Recommendations ===")
	t.Log("")
	t.Log("If building bb inside Docker for deployment on a different machine:")
	t.Log("1. Use -DTARGET_ARCH=x86-64-v3 for AVX2 support (most modern CPUs)")
	t.Log("2. Use -DTARGET_ARCH=x86-64-v4 for AVX-512 support (newer CPUs)")
	t.Log("3. Use -DTARGET_ARCH=native ONLY if building on the deployment machine")
	t.Log("")
	t.Log("Current Dockerfile uses -DTARGET_ARCH=native which may not be optimal")
	t.Log("if the Docker image is built on a different machine than where it runs.")
	t.Log("")
	t.Log("=== Multithreading Recommendations ===")
	t.Log("")
	t.Logf("CPU has %d cores available", cpuInfo.NumCPUs)
	t.Log("Ensure bb is compiled with:")
	t.Log("  -DMULTITHREADING=ON")
	t.Log("  -DENABLE_PAR_ALGOS=ON")
}

// TestDockerBuildOptimizations provides specific Docker build recommendations
func TestDockerBuildOptimizations(t *testing.T) {
	t.Log("=== Recommended Dockerfile Changes ===")
	t.Log("")
	t.Log("For optimal performance across different deployment targets:")
	t.Log("")
	t.Log("Option 1: Build for x86-64-v3 (AVX2, most compatible):")
	t.Log("  cmake --preset clang20 \\")
	t.Log("    -DCMAKE_BUILD_TYPE=Release \\")
	t.Log("    -DTARGET_ARCH=x86-64-v3 \\")
	t.Log("    -DENABLE_PAR_ALGOS=ON \\")
	t.Log("    -DMULTITHREADING=ON \\")
	t.Log("    -DDISABLE_AZTEC_VM=ON \\")
	t.Log("    -DCMAKE_CXX_FLAGS=\"-O3\"")
	t.Log("")
	t.Log("Option 2: Build for x86-64-v4 (AVX-512, newer CPUs):")
	t.Log("  cmake --preset clang20 \\")
	t.Log("    -DCMAKE_BUILD_TYPE=Release \\")
	t.Log("    -DTARGET_ARCH=x86-64-v4 \\")
	t.Log("    -DENABLE_PAR_ALGOS=ON \\")
	t.Log("    -DMULTITHREADING=ON \\")
	t.Log("    -DDISABLE_AZTEC_VM=ON \\")
	t.Log("    -DCMAKE_CXX_FLAGS=\"-O3\"")
	t.Log("")
	t.Log("Option 3: Multi-architecture build (build multiple binaries):")
	t.Log("  Build separate images for different CPU architectures")
	t.Log("  and use runtime detection to select the optimal binary.")
	t.Log("")
	t.Log("=== x86-64 Microarchitecture Levels ===")
	t.Log("  x86-64    : Baseline (SSE2)")
	t.Log("  x86-64-v2 : +SSE3, SSSE3, SSE4.1, SSE4.2, POPCNT")
	t.Log("  x86-64-v3 : +AVX, AVX2, BMI1, BMI2, FMA (recommended)")
	t.Log("  x86-64-v4 : +AVX-512 (best performance on supported CPUs)")
}

// TestAllocatorOptimizations provides recommendations for memory allocator optimization
func TestAllocatorOptimizations(t *testing.T) {
	t.Log("=== Memory Allocator Optimization ===")
	t.Log("")
	t.Log("Based on Aztec/Barretenberg benchmarks, using alternative memory allocators")
	t.Log("can provide significant speedups (10-60% depending on core count):")
	t.Log("")
	t.Log("| Allocator      | Improvement vs glibc |")
	t.Log("|----------------|---------------------|")
	t.Log("| tcmalloc       | ~11% (32 cores), ~60% (192 cores) |")
	t.Log("| jemalloc       | ~6% (32 cores)      |")
	t.Log("| mimalloc       | ~0% (proving), better tracegen |")
	t.Log("")
	t.Log("The improvement comes from reduced lock contention in memory allocation")
	t.Log("when multiple threads compete for memory. tcmalloc's per-thread caching")
	t.Log("is particularly effective.")
	t.Log("")
	t.Log("=== How to Use Alternative Allocators ===")
	t.Log("")
	t.Log("Option 1: LD_PRELOAD (no rebuild required):")
	t.Log("  # Install tcmalloc")
	t.Log("  apt-get install libtcmalloc-minimal4")
	t.Log("")
	t.Log("  # Run bb with tcmalloc")
	t.Log("  LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libtcmalloc_minimal.so.4 bb prove ...")
	t.Log("")
	t.Log("Option 2: Docker entrypoint modification:")
	t.Log("  # In Dockerfile, add:")
	t.Log("  RUN apt-get update && apt-get install -y libtcmalloc-minimal4")
	t.Log("  ENV LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libtcmalloc_minimal.so.4")
	t.Log("")
	t.Log("Option 3: Link at compile time (requires rebuild):")
	t.Log("  cmake ... -DCMAKE_EXE_LINKER_FLAGS=\"-ltcmalloc_minimal\"")
	t.Log("")
	t.Log("=== When Allocator Optimization Helps Most ===")
	t.Log("")
	t.Log("- High core count systems (32+ cores): Significant improvement")
	t.Log("- Low core count systems (8-16 cores): Minimal improvement")
	t.Log("- Memory-intensive operations: Commitment computations, polynomial operations")
	t.Log("")
	t.Log("Reference: https://github.com/AztecProtocol/barretenberg/issues/1617")
}

// TestThreadingLimitations documents known threading limitations
func TestThreadingLimitations(t *testing.T) {
	t.Log("=== Known Threading Limitations in Barretenberg ===")
	t.Log("")
	t.Log("The bb prover does NOT fully utilize all CPU cores due to algorithmic")
	t.Log("limitations. This is a known issue being actively worked on.")
	t.Log("")
	t.Log("Observed behavior:")
	t.Log("- On 32-core machines: ~85-100% CPU utilization")
	t.Log("- On 44-core machines: Similar underutilization")
	t.Log("- Proof generation time doesn't scale linearly with core count")
	t.Log("")
	t.Log("Root causes:")
	t.Log("1. Some algorithms can't be parallelized efficiently")
	t.Log("2. Memory allocation contention (see allocator recommendations)")
	t.Log("3. Sequential dependencies in proof construction")
	t.Log("")
	t.Log("Mitigation strategies:")
	t.Log("1. Use tcmalloc to reduce allocation contention")
	t.Log("2. Ensure MULTITHREADING=ON and ENABLE_PAR_ALGOS=ON at compile time")
	t.Log("3. For multiple concurrent proofs, run separate processes")
	t.Log("")
	t.Log("Reference: https://github.com/AztecProtocol/aztec-packages/issues/15614")
}

// TestMeasureProverOverhead measures the overhead of the prover service wrapper
func TestMeasureProverOverhead(t *testing.T) {
	// This test measures how much overhead the Go service adds
	// compared to calling the prover binary directly

	proverPath := os.Getenv("VOCDONI_PROVER_BINARY_PATH")
	if proverPath == "" {
		t.Skip("VOCDONI_PROVER_BINARY_PATH not set")
	}

	// Measure just the binary startup time
	start := time.Now()
	cmd := exec.Command(proverPath, "--help")
	if err := cmd.Run(); err != nil {
		t.Fatalf("prover-cli --help failed: %v", err)
	}
	startupTime := time.Since(start)

	t.Logf("Prover CLI startup time: %v", startupTime)
	t.Log("")
	t.Log("Note: The actual proving time is dominated by the cryptographic")
	t.Log("operations, not the Go service overhead. Focus optimization efforts")
	t.Log("on the bb binary compilation flags and CPU architecture matching.")
}

// TestWriteBenchmarkScript creates a shell script for manual benchmarking
func TestWriteBenchmarkScript(t *testing.T) {
	scriptContent := `#!/bin/bash
# Barretenberg Prover Benchmark Script
# Run this script to benchmark the outer proof generation

set -e

# Configuration
BB_PATH="${BB_BINARY_PATH:-$(which bb)}"
PROVER_PATH="${VOCDONI_PROVER_BINARY_PATH:-}"
ARTIFACTS_DIR="${VOCDONI_ARTIFACTS_DIR:-}"
REQUEST_FILE="${1:-}"

if [ -z "$BB_PATH" ]; then
    echo "Error: bb binary not found. Set BB_BINARY_PATH or ensure bb is in PATH"
    exit 1
fi

echo "=== System Information ==="
echo "CPU: $(grep 'model name' /proc/cpuinfo | head -1 | cut -d: -f2 | xargs)"
echo "Cores: $(nproc)"
echo "BB Path: $BB_PATH"
echo "BB Version: $($BB_PATH --version 2>/dev/null || echo 'unknown')"
echo ""

echo "=== CPU SIMD Flags ==="
grep flags /proc/cpuinfo | head -1 | tr ' ' '\n' | grep -E '^(sse|avx|fma|bmi)' | sort -u | tr '\n' ' '
echo ""
echo ""

echo "=== BB Binary SIMD Analysis ==="
echo "AVX-512 (zmm) instructions: $(objdump -d $BB_PATH 2>/dev/null | grep -c zmm || echo 0)"
echo "AVX2 (ymm) instructions: $(objdump -d $BB_PATH 2>/dev/null | grep -c ymm || echo 0)"
echo "AVX (vmov) instructions: $(objdump -d $BB_PATH 2>/dev/null | grep -c vmov || echo 0)"
echo ""

if [ -n "$PROVER_PATH" ] && [ -n "$ARTIFACTS_DIR" ] && [ -n "$REQUEST_FILE" ]; then
    echo "=== Running Benchmark ==="
    echo "Request file: $REQUEST_FILE"
    echo ""
    
    OUTPUT_FILE=$(mktemp)
    BENCH_FILE=$(mktemp)
    
    echo "Starting benchmark at $(date)"
    START_TIME=$(date +%s.%N)
    
    $PROVER_PATH aggregate-request \
        --input "$REQUEST_FILE" \
        --artifacts-dir "$ARTIFACTS_DIR" \
        --output "$OUTPUT_FILE" 2>&1 | tee "$BENCH_FILE"
    
    END_TIME=$(date +%s.%N)
    DURATION=$(echo "$END_TIME - $START_TIME" | bc)
    
    echo ""
    echo "=== Benchmark Results ==="
    echo "Duration: ${DURATION}s"
    
    rm -f "$OUTPUT_FILE" "$BENCH_FILE"
else
    echo "=== Skipping Benchmark ==="
    echo "To run a benchmark, set:"
    echo "  VOCDONI_PROVER_BINARY_PATH"
    echo "  VOCDONI_ARTIFACTS_DIR"
    echo "  And provide a request file as argument"
fi

echo ""
echo "=== Optimization Recommendations ==="
echo "1. Ensure bb is compiled with -DTARGET_ARCH matching your CPU"
echo "2. Use -DENABLE_PAR_ALGOS=ON for parallel algorithms"
echo "3. Use -DMULTITHREADING=ON for multi-threading"
echo "4. For Docker builds, use x86-64-v3 or x86-64-v4 instead of native"
`

	scriptPath := filepath.Join(os.TempDir(), "bb_benchmark.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to write benchmark script: %v", err)
	}

	t.Logf("Benchmark script written to: %s", scriptPath)
	t.Log("Run it with: bash " + scriptPath)
	t.Log("")
	t.Log("Or copy to the server-go directory:")
	fmt.Printf("cp %s ./scripts/bb_benchmark.sh\n", scriptPath)
}
