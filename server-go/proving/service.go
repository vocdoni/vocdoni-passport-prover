package proving

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

type InnerProof struct {
	CircuitName  string   `json:"circuitName"`
	Proof        []string `json:"proof"`
	PublicInputs []string `json:"publicInputs"`
	Vkey         []string `json:"vkey,omitempty"`
	KeyHash      string   `json:"keyHash"`
	TreeHashPath []string `json:"treeHashPath"`
	TreeIndex    string   `json:"treeIndex"`
}

type AggregateRequest struct {
	Version     string         `json:"version"`
	CurrentDate int64          `json:"currentDate"`
	DSC         InnerProof     `json:"dsc"`
	IDData      InnerProof     `json:"idData"`
	Integrity   InnerProof     `json:"integrity"`
	Disclosures []InnerProof   `json:"disclosures"`
	Request     map[string]any `json:"request,omitempty"`
}

type AggregateResponse struct {
	Version      string            `json:"version"`
	Name         string            `json:"name"`
	Proof        string            `json:"proof"`
	PublicInputs []string          `json:"publicInputs"`
	VkeyHash     string            `json:"vkeyHash"`
	Nullifier    string            `json:"nullifier,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type aggregateCLIResponse struct {
	Version      string            `json:"version"`
	Name         string            `json:"name"`
	Proof        string            `json:"proof"`
	PublicInputs []string          `json:"public_inputs"`
	VkeyHash     string            `json:"vkey_hash"`
	Nullifier    string            `json:"nullifier,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Config struct {
	ProverBinaryPath string
	BBBinaryPath     string
	ArtifactsDir     string
	WorkspaceRoot    string
	LowMemoryMode    bool
	MaxStorageUsage  *uint64
	Timeout          time.Duration
	MaxConcurrency   int
}

type Service struct {
	proverBinaryPath string
	bbBinaryPath     string
	artifactsDir     string
	workspaceRoot    string
	logger           zerolog.Logger
	lowMemoryMode    bool
	maxStorageUsage  *uint64
	timeout          time.Duration
	slots            chan struct{}
	inflight         atomic.Int64
}

func NewService(cfg Config, logger zerolog.Logger) *Service {
	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	return &Service{
		proverBinaryPath: cfg.ProverBinaryPath,
		bbBinaryPath:     cfg.BBBinaryPath,
		artifactsDir:     cfg.ArtifactsDir,
		workspaceRoot:    cfg.WorkspaceRoot,
		logger:           logger.With().Str("component", "prover").Logger(),
		lowMemoryMode:    cfg.LowMemoryMode,
		maxStorageUsage:  cfg.MaxStorageUsage,
		timeout:          cfg.Timeout,
		slots:            make(chan struct{}, maxConcurrency),
	}
}

func (s *Service) Aggregate(ctx context.Context, req AggregateRequest) (*AggregateResponse, error) {
	if req.Version == "" {
		return nil, fmt.Errorf("missing version")
	}
	if req.DSC.CircuitName == "" || req.IDData.CircuitName == "" || req.Integrity.CircuitName == "" {
		return nil, fmt.Errorf("missing one or more recursive inner proofs")
	}
	if len(req.Disclosures) == 0 {
		return nil, fmt.Errorf("missing disclosures")
	}
	if strings.TrimSpace(s.proverBinaryPath) == "" {
		return nil, fmt.Errorf("prover binary path is not configured")
	}
	if strings.TrimSpace(s.artifactsDir) == "" {
		return nil, fmt.Errorf("artifacts directory is not configured")
	}
	start := time.Now()
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.release()

	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	requestFile, err := os.CreateTemp("", "vocdoni-aggregate-request-*.json")
	if err != nil {
		return nil, fmt.Errorf("create temporary aggregate request file: %w", err)
	}
	requestPath := requestFile.Name()
	defer os.Remove(requestPath)

	if err := json.NewEncoder(requestFile).Encode(req); err != nil {
		requestFile.Close()
		return nil, fmt.Errorf("write aggregate request file: %w", err)
	}
	if err := requestFile.Close(); err != nil {
		return nil, fmt.Errorf("close aggregate request file: %w", err)
	}

	outputFile, err := os.CreateTemp("", "vocdoni-aggregate-response-*.json")
	if err != nil {
		return nil, fmt.Errorf("create temporary aggregate response file: %w", err)
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	cmd := exec.CommandContext(
		ctx,
		s.proverBinaryPath,
		"aggregate-request",
		"--input", requestPath,
		"--artifacts-dir", s.artifactsDir,
		"--output", outputPath,
	)
	if s.lowMemoryMode {
		cmd.Args = append(cmd.Args, "--low-memory-mode")
	}
	if s.maxStorageUsage != nil {
		cmd.Args = append(cmd.Args, "--max-storage-usage", fmt.Sprintf("%d", *s.maxStorageUsage))
	}
	cmd.Env = append(os.Environ(), s.commandEnv()...)
	if s.workspaceRoot != "" {
		cmd.Dir = s.workspaceRoot
	}

	s.logger.Info().
		Str("prover_binary", s.proverBinaryPath).
		Str("bb_binary", s.bbBinaryPath).
		Str("artifacts_dir", s.artifactsDir).
		Str("workspace_root", s.workspaceRoot).
		Bool("low_memory_mode", s.lowMemoryMode).
		Uint64("max_storage_usage", derefUint64(s.maxStorageUsage)).
		Dur("timeout", s.timeout).
		Int("queue_capacity", cap(s.slots)).
		Int64("inflight_requests", s.inflight.Load()).
		Str("request_file", requestPath).
		Str("output_file", outputPath).
		Str("dsc_circuit", req.DSC.CircuitName).
		Str("id_data_circuit", req.IDData.CircuitName).
		Str("integrity_circuit", req.Integrity.CircuitName).
		Int("disclosures", len(req.Disclosures)).
		Msg("starting aggregate prover command")

	output, err := cmd.CombinedOutput()
	if err != nil {
		reason := strings.TrimSpace(string(output))
		errEvent := s.logger.Error().
			Err(err).
			Dur("duration", time.Since(start)).
			Str("command_output", reason)
		if isLikelyOOMKill(err) {
			errEvent = errEvent.Str("hint", "subprocess received SIGKILL; this is typically an OOM kill, enable low-memory mode or reduce concurrency")
		}
		errEvent.Msg("aggregate prover command failed")
		if isLikelyOOMKill(err) {
			return nil, fmt.Errorf("aggregate prove command failed: %w (likely OOM kill)", err)
		}
		return nil, fmt.Errorf("aggregate prove command failed: %w: %s", err, reason)
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("read aggregate response file: %w", err)
	}
	var cliResponse aggregateCLIResponse
	if err := json.Unmarshal(raw, &cliResponse); err != nil {
		return nil, fmt.Errorf("decode aggregate response: %w", err)
	}
	response := AggregateResponse{
		Version:      cliResponse.Version,
		Name:         cliResponse.Name,
		Proof:        cliResponse.Proof,
		PublicInputs: cliResponse.PublicInputs,
		VkeyHash:     cliResponse.VkeyHash,
		Nullifier:    cliResponse.Nullifier,
		Metadata:     cliResponse.Metadata,
	}
	if response.Proof == "" || len(response.PublicInputs) == 0 {
		return nil, fmt.Errorf("aggregate response missing proof data")
	}
	if verified, ok := response.Metadata["proof_verified"]; ok && !strings.EqualFold(verified, "true") {
		return nil, fmt.Errorf("aggregate response verification failed")
	}
	s.logger.Info().
		Dur("duration", time.Since(start)).
		Str("proof_name", response.Name).
		Str("version", response.Version).
		Str("nullifier", response.Nullifier).
		Int("public_inputs", len(response.PublicInputs)).
		Str("vkey_hash", response.VkeyHash).
		Msg("aggregate prover command succeeded")
	return &response, nil
}

func (s *Service) commandEnv() []string {
	env := make([]string, 0, 2)
	if s.bbBinaryPath != "" {
		env = append(env, "BB_BINARY_PATH="+s.bbBinaryPath)
	}
	if s.workspaceRoot != "" {
		env = append(env, "VOCDONI_WORKSPACE_ROOT="+filepath.Clean(s.workspaceRoot))
	}
	return env
}

func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.slots <- struct{}{}:
		s.inflight.Add(1)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("aggregate queue cancelled: %w", ctx.Err())
	}
}

func (s *Service) release() {
	select {
	case <-s.slots:
		s.inflight.Add(-1)
	default:
	}
}

func derefUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func isLikelyOOMKill(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}
	return status.Signaled() && status.Signal() == syscall.SIGKILL
}
