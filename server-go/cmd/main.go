package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/vocdoni/vocdoni-passport-prover/server-go/api"
	"github.com/vocdoni/vocdoni-passport-prover/server-go/proving"
)

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func buildLogger() zerolog.Logger {
	level, err := zerolog.ParseLevel(strings.ToLower(envOrDefault("VOCDONI_LOG_LEVEL", "info")))
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339Nano
	return zerolog.New(os.Stdout).With().
		Timestamp().
		Str("service", "vocdoni-passport-server").
		Logger()
}

func envBoolOrDefault(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envIntOrDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envUint64Ptr(key string) *uint64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func main() {
	logger := buildLogger()

	listenAddr := flag.String("listen", envOrDefault("VOCDONI_SERVER_LISTEN", "0.0.0.0:8080"), "HTTP listen address")
	apkPath := flag.String("apk-path", envOrDefault("VOCDONI_APK_PATH", "/opt/vocdoni/downloads/app-release.apk"), "Path to the Android APK served by the download endpoint")
	proverBinaryPath := flag.String("prover-binary", envOrDefault("VOCDONI_PROVER_BINARY_PATH", "/opt/vocdoni/bin/prover-cli"), "Path to the prover CLI binary")
	bbBinaryPath := flag.String("bb-binary", envOrDefault("BB_BINARY_PATH", "/usr/local/bin/bb"), "Path to the zkPassport bb binary")
	artifactsDir := flag.String("artifacts-dir", envOrDefault("VOCDONI_ARTIFACTS_DIR", "/opt/vocdoni/repos/vocdoni-passport-prover/artifacts/registry/minimal-default-0.16.0"), "Path to the packaged circuit artifacts directory")
	workspaceRoot := flag.String("workspace-root", envOrDefault("VOCDONI_WORKSPACE_ROOT", "/opt/vocdoni/repos/vocdoni-passport-prover"), "Workspace root used by the prover CLI for scripts and caches")
	proverLowMemoryMode := flag.Bool("prover-low-memory", envBoolOrDefault("VOCDONI_PROVER_LOW_MEMORY_MODE", true), "Enable low-memory mode for aggregate proving")
	proverMaxConcurrency := flag.Int("prover-max-concurrency", envIntOrDefault("VOCDONI_PROVER_MAX_CONCURRENCY", 1), "Maximum concurrent aggregate prover jobs")
	proverTimeout := flag.Duration("prover-timeout", envDurationOrDefault("VOCDONI_PROVER_TIMEOUT", 20*time.Minute), "Timeout for a single aggregate prover job")
	flag.Parse()
	proverMaxStorageUsage := envUint64Ptr("VOCDONI_PROVER_MAX_STORAGE_USAGE")

	logger.Info().
		Str("listen_addr", *listenAddr).
		Str("public_base_url", envOrDefault("VOCDONI_PUBLIC_BASE_URL", "")).
		Str("apk_path", *apkPath).
		Str("prover_binary", *proverBinaryPath).
		Str("bb_binary", *bbBinaryPath).
		Str("artifacts_dir", *artifactsDir).
		Str("workspace_root", *workspaceRoot).
		Bool("prover_low_memory_mode", *proverLowMemoryMode).
		Int("prover_max_concurrency", *proverMaxConcurrency).
		Dur("prover_timeout", *proverTimeout).
		Uint64("prover_max_storage_usage", func() uint64 {
			if proverMaxStorageUsage == nil {
				return 0
			}
			return *proverMaxStorageUsage
		}()).
		Msg("initializing server")

	provingService := proving.NewService(proving.Config{
		ProverBinaryPath: *proverBinaryPath,
		BBBinaryPath:     *bbBinaryPath,
		ArtifactsDir:     *artifactsDir,
		WorkspaceRoot:    *workspaceRoot,
		LowMemoryMode:    *proverLowMemoryMode,
		MaxStorageUsage:  proverMaxStorageUsage,
		Timeout:          *proverTimeout,
		MaxConcurrency:   *proverMaxConcurrency,
	}, logger)

	server := api.NewServer(*listenAddr, provingService, *apkPath, logger)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info().Str("signal", sig.String()).Msg("shutdown requested")
	case err := <-errCh:
		if err != nil {
			logger.Fatal().Err(err).Msg("server exited with error")
		}
		logger.Info().Msg("server stopped")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("server shutdown failed")
		os.Exit(1)
	}
	logger.Info().Msg("server stopped cleanly")
}
