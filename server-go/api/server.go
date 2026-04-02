package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/vocdoni/vocdoni-passport-prover/server-go/proving"
)

type Server struct {
	httpServer     *http.Server
	logger         zerolog.Logger
	provingService *proving.Service
	apkPath        string
}

func NewServer(listenAddr string, provingService *proving.Service, apkPath string, logger zerolog.Logger) *Server {
	s := &Server{
		logger:         logger.With().Str("component", "http").Logger(),
		provingService: provingService,
		apkPath:        strings.TrimSpace(apkPath),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleRequestPage)
	mux.HandleFunc("GET /api/request-config", s.handleRequestConfig)
	mux.HandleFunc("GET /api/request-qr.png", s.handleRequestQR)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /downloads/app-release.apk", s.handleAPKDownload)
	mux.HandleFunc("POST /api/proofs/aggregate", s.handleAggregateProofs)

	handler := recoveryMiddleware(s.logger, requestLoggingMiddleware(s.logger, corsMiddleware(mux)))
	s.httpServer = &http.Server{
		Addr:              listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
	s.logger.Info().
		Str("listen_addr", s.httpServer.Addr).
		Msg("starting HTTP server")
	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func recoveryMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error().
					Interface("panic", rec).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Msg("request panicked")
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func requestLoggingMiddleware(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)

		event := logger.Info()
		if ww.status >= http.StatusInternalServerError {
			event = logger.Error()
		} else if ww.status >= http.StatusBadRequest {
			event = logger.Warn()
		}

		event.
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.status).
			Int("bytes", ww.bytes).
			Dur("duration", time.Since(start)).
			Str("remote_ip", remoteIP(r)).
			Str("user_agent", r.UserAgent()).
			Msg("request completed")
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "vocdoni-passport-server",
	})
}

func (s *Server) handleAPKDownload(w http.ResponseWriter, r *http.Request) {
	if s.apkPath == "" {
		writeError(w, http.StatusNotFound, "apk download not configured")
		return
	}
	info, err := os.Stat(s.apkPath)
	if err != nil || info.IsDir() {
		s.logger.Warn().
			Err(err).
			Str("apk_path", s.apkPath).
			Msg("apk download requested but file is unavailable")
		writeError(w, http.StatusNotFound, "apk file not available")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="vocdoni-passport.apk"`)
	http.ServeFile(w, r, s.apkPath)
}

func (s *Server) handleAggregateProofs(w http.ResponseWriter, r *http.Request) {
	if s.provingService == nil {
		writeError(w, http.StatusServiceUnavailable, "proving service not configured")
		return
	}

	var req proving.AggregateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	logEvent := s.logger.Info().
		Str("version", req.Version).
		Str("dsc_circuit", req.DSC.CircuitName).
		Str("id_data_circuit", req.IDData.CircuitName).
		Str("integrity_circuit", req.Integrity.CircuitName).
		Int("disclosures", len(req.Disclosures))
	if req.Request != nil {
		if petitionID, ok := req.Request["petitionId"].(string); ok && petitionID != "" {
			logEvent = logEvent.Str("petition_id", petitionID)
		}
	}
	logEvent.Msg("aggregate request received")

	resp, err := s.provingService.Aggregate(r.Context(), req)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("version", req.Version).
			Msg("aggregate request failed")
		writeError(w, http.StatusBadRequest, "aggregate failed: "+err.Error())
		return
	}

	s.logger.Info().
		Str("proof_name", resp.Name).
		Str("version", resp.Version).
		Int("public_inputs", len(resp.PublicInputs)).
		Str("vkey_hash", resp.VkeyHash).
		Str("nullifier", resp.Nullifier).
		Msg("aggregate request completed")
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func remoteIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
