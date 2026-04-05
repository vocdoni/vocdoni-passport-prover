package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/vocdoni/vocdoni-passport-prover/server-go/presets"
	"github.com/vocdoni/vocdoni-passport-prover/server-go/proving"
	"github.com/vocdoni/vocdoni-passport-prover/server-go/storage"
)

type Server struct {
	httpServer     *http.Server
	logger         zerolog.Logger
	provingService *proving.Service
	storage        *storage.MongoDB
	apkPath        string
}

func NewServer(listenAddr string, provingService *proving.Service, db *storage.MongoDB, apkPath string, logger zerolog.Logger) *Server {
	s := &Server{
		logger:         logger.With().Str("component", "http").Logger(),
		provingService: provingService,
		storage:        db,
		apkPath:        strings.TrimSpace(apkPath),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleRequestPage)
	mux.HandleFunc("GET /api/request-config", s.handleRequestConfig)
	mux.HandleFunc("GET /api/request-qr.png", s.handleRequestQR)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /downloads/app-release.apk", s.handleAPKDownload)
	mux.HandleFunc("POST /api/proofs/aggregate", s.handleAggregateProofs)

	mux.HandleFunc("POST /api/petitions", s.handleCreatePetition)
	mux.HandleFunc("GET /api/petitions", s.handleListPetitions)
	mux.HandleFunc("GET /api/petitions/{id}", s.handleGetPetition)
	mux.HandleFunc("GET /api/petitions/{id}/signatures", s.handleGetPetitionSignatures)

	mux.HandleFunc("GET /api/presets", s.handleListPresets)
	mux.HandleFunc("GET /api/presets/{id}", s.handleGetPreset)
	mux.HandleFunc("GET /api/presets/{id}/fields", s.handleGetPresetFields)

	mux.HandleFunc("GET /petition/{id}", s.handlePetition)
	mux.HandleFunc("GET /api/petition-qr.png", s.handlePetitionQR)
	mux.HandleFunc("GET /explore", s.handleExplorePage)
	mux.HandleFunc("GET /about", s.handleAboutPage)

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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
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

	var petitionID string
	var petition *storage.Petition
	var disclosedFields []string

	if req.Request != nil {
		if pid, ok := req.Request["petitionId"].(string); ok && pid != "" {
			petitionID = pid
		}
		if query, ok := req.Request["query"].(map[string]any); ok {
			disclosedFields = collectDisclosedFieldsFromQuery(query)
		}
	}

	// Extract nullifier and signer address from inner proofs BEFORE expensive aggregation
	// The nullifier is at publicInputs[6] of disclosure proofs (scoped_nullifier)
	// The signer address is at publicInputs[7] of bind_evm circuit
	nullifier := extractNullifierFromDisclosures(req.Disclosures)
	signerAddress := extractSignerAddressFromDisclosures(req.Disclosures)

	logEvent := s.logger.Info().
		Str("version", req.Version).
		Str("dsc_circuit", req.DSC.CircuitName).
		Str("id_data_circuit", req.IDData.CircuitName).
		Str("integrity_circuit", req.Integrity.CircuitName).
		Int("disclosures", len(req.Disclosures)).
		Strs("disclosed_fields", disclosedFields).
		Str("nullifier", nullifier).
		Str("signer_address", signerAddress)

	if petitionID != "" {
		logEvent = logEvent.Str("petition_id", petitionID)

		if s.storage != nil {
			var err error
			petition, err = s.storage.GetPetition(r.Context(), petitionID)
			if err != nil {
				s.logger.Warn().Err(err).Str("petition_id", petitionID).Msg("failed to fetch petition")
			} else if petition != nil {
				if err := validateDisclosedFieldsAgainstPetition(disclosedFields, petition); err != nil {
					s.logger.Warn().
						Err(err).
						Str("petition_id", petitionID).
						Strs("required_fields", petition.DisclosedFields).
						Strs("provided_fields", disclosedFields).
						Msg("disclosed fields validation failed")
					writeError(w, http.StatusBadRequest, "disclosed fields validation failed: "+err.Error())
					return
				}
			}

			// Check for duplicate signature BEFORE running expensive aggregation
			if nullifier != "" {
				exists, err := s.storage.SignatureExists(r.Context(), petitionID, nullifier)
				if err != nil {
					s.logger.Warn().Err(err).Msg("failed to check signature existence")
				} else if exists {
					s.logger.Warn().
						Str("petition_id", petitionID).
						Str("nullifier", nullifier).
						Msg("duplicate signature rejected (pre-aggregation check)")
					writeError(w, http.StatusConflict, "signature already exists for this petition")
					return
				}
			}
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

	// Save signature to storage (duplicate check was done before aggregation)
	if s.storage != nil && petitionID != "" && resp.Nullifier != "" {
		sig := &storage.Signature{
			PetitionID:      petitionID,
			Nullifier:       resp.Nullifier,
			SignerAddress:   signerAddress,
			ProofHash:       resp.VkeyHash,
			DisclosedFields: disclosedFields,
			Version:         resp.Version,
		}
		if err := s.storage.SaveSignature(r.Context(), sig); err != nil {
			// If save fails due to duplicate (race condition), reject
			if strings.Contains(err.Error(), "duplicate") {
				s.logger.Warn().
					Str("petition_id", petitionID).
					Str("nullifier", resp.Nullifier).
					Msg("duplicate signature rejected (race condition)")
				writeError(w, http.StatusConflict, "signature already exists for this petition")
				return
			}
			s.logger.Warn().Err(err).Msg("failed to save signature")
		} else {
			s.logger.Info().
				Str("petition_id", petitionID).
				Str("nullifier", resp.Nullifier).
				Str("signer_address", signerAddress).
				Strs("disclosed_fields", disclosedFields).
				Msg("signature saved")
		}
	}

	s.logger.Info().
		Str("proof_name", resp.Name).
		Str("version", resp.Version).
		Int("public_inputs", len(resp.PublicInputs)).
		Str("vkey_hash", resp.VkeyHash).
		Str("nullifier", resp.Nullifier).
		Strs("disclosed_fields", disclosedFields).
		Msg("aggregate request completed")
	writeJSON(w, http.StatusOK, resp)
}

func collectDisclosedFieldsFromQuery(query map[string]any) []string {
	if query == nil {
		return nil
	}
	var fields []string
	for key, value := range query {
		if m, ok := value.(map[string]any); ok {
			if disclose, ok := m["disclose"].(bool); ok && disclose {
				fields = append(fields, key)
			}
			if _, ok := m["eq"]; ok {
				fields = append(fields, key)
			}
		}
	}
	return fields
}

// extractNullifierFromDisclosures extracts the scoped_nullifier from disclosure proofs.
// According to zkPassport, the nullifier is at publicInputs[6] of disclosure proofs.
// We look for the first non-zero nullifier among all disclosure proofs.
func extractNullifierFromDisclosures(disclosures []proving.InnerProof) string {
	for _, d := range disclosures {
		if len(d.PublicInputs) > 6 {
			nullifier := d.PublicInputs[6]
			// Check if it's non-zero (not "0x0" or "0" or empty)
			if nullifier != "" && nullifier != "0" && nullifier != "0x0" &&
				nullifier != "0x0000000000000000000000000000000000000000000000000000000000000000" {
				return nullifier
			}
		}
	}
	return ""
}

// extractSignerAddressFromDisclosures extracts the user_address from bind_evm circuit.
// The bind_evm circuit has the bound user_address in publicInputs[7].
// The address is a 20-byte Ethereum address represented as a hex field element.
func extractSignerAddressFromDisclosures(disclosures []proving.InnerProof) string {
	for _, d := range disclosures {
		if strings.Contains(d.CircuitName, "bind") && len(d.PublicInputs) > 7 {
			addr := d.PublicInputs[7]
			// Check if it's non-zero
			if addr != "" && addr != "0" && addr != "0x0" &&
				addr != "0x0000000000000000000000000000000000000000000000000000000000000000" {
				// Convert from field element to Ethereum address format
				return normalizeEthAddress(addr)
			}
		}
	}
	return ""
}

// normalizeEthAddress converts a hex field element to a standard Ethereum address.
// Input can be "0x..." with up to 64 hex chars, output is "0x" + 40 hex chars (20 bytes).
func normalizeEthAddress(hexVal string) string {
	// Remove 0x prefix if present
	addr := strings.TrimPrefix(hexVal, "0x")
	// Ethereum addresses are 20 bytes = 40 hex chars
	// The field element might be padded, so take the last 40 chars
	if len(addr) > 40 {
		addr = addr[len(addr)-40:]
	} else if len(addr) < 40 {
		// Pad with leading zeros if needed
		addr = strings.Repeat("0", 40-len(addr)) + addr
	}
	return "0x" + addr
}

func validateDisclosedFieldsAgainstPetition(providedFields []string, petition *storage.Petition) error {
	if petition == nil || len(petition.DisclosedFields) == 0 {
		return nil
	}

	providedSet := make(map[string]bool)
	for _, f := range providedFields {
		providedSet[f] = true
	}

	var missing []string
	for _, required := range petition.DisclosedFields {
		if !providedSet[required] {
			missing = append(missing, required)
		}
	}

	if len(missing) > 0 {
		return errors.New("missing required disclosed fields: " + strings.Join(missing, ", "))
	}

	return nil
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

type CreatePetitionRequest struct {
	Name    string         `json:"name"`
	Purpose string         `json:"purpose"`
	Scope   string         `json:"scope"`
	Query   map[string]any `json:"query"`
	Preset  string         `json:"preset,omitempty"`
}

func (s *Server) handleCreatePetition(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		writeError(w, http.StatusServiceUnavailable, "petition storage not configured")
		return
	}

	var req CreatePetitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	petition := &storage.Petition{
		Name:    req.Name,
		Purpose: req.Purpose,
		Scope:   req.Scope,
		Query:   req.Query,
		Preset:  req.Preset,
	}

	if err := s.storage.CreatePetition(r.Context(), petition); err != nil {
		s.logger.Error().Err(err).Msg("failed to create petition")
		writeError(w, http.StatusInternalServerError, "failed to create petition: "+err.Error())
		return
	}

	s.logger.Info().
		Str("petition_id", petition.PetitionID).
		Str("name", petition.Name).
		Strs("disclosed_fields", petition.DisclosedFields).
		Msg("petition created")

	writeJSON(w, http.StatusCreated, petition)
}

func (s *Server) handleListPetitions(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		writeError(w, http.StatusServiceUnavailable, "petition storage not configured")
		return
	}

	limit := int64(20)
	offset := int64(0)

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 64); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.ParseInt(o, 10, 64); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	petitions, total, err := s.storage.ListPetitions(r.Context(), limit, offset)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list petitions")
		writeError(w, http.StatusInternalServerError, "failed to list petitions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"petitions": petitions,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}

func (s *Server) handleGetPetition(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		writeError(w, http.StatusServiceUnavailable, "petition storage not configured")
		return
	}

	petitionID := r.PathValue("id")
	if petitionID == "" {
		writeError(w, http.StatusBadRequest, "petition ID is required")
		return
	}

	petition, err := s.storage.GetPetition(r.Context(), petitionID)
	if err != nil {
		s.logger.Error().Err(err).Str("petition_id", petitionID).Msg("failed to get petition")
		writeError(w, http.StatusInternalServerError, "failed to get petition")
		return
	}

	if petition == nil {
		writeError(w, http.StatusNotFound, "petition not found")
		return
	}

	writeJSON(w, http.StatusOK, petition)
}

func (s *Server) handleGetPetitionSignatures(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		writeError(w, http.StatusServiceUnavailable, "petition storage not configured")
		return
	}

	petitionID := r.PathValue("id")
	if petitionID == "" {
		writeError(w, http.StatusBadRequest, "petition ID is required")
		return
	}

	limit := int64(50)
	offset := int64(0)

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 64); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.ParseInt(o, 10, 64); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	signatures, total, err := s.storage.GetSignaturesByPetition(r.Context(), petitionID, limit, offset)
	if err != nil {
		s.logger.Error().Err(err).Str("petition_id", petitionID).Msg("failed to get signatures")
		writeError(w, http.StatusInternalServerError, "failed to get signatures")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"signatures": signatures,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

func (s *Server) handleListPresets(w http.ResponseWriter, r *http.Request) {
	summaries := presets.ListPresetSummaries()
	writeJSON(w, http.StatusOK, map[string]any{
		"presets": summaries,
	})
}

func (s *Server) handleGetPreset(w http.ResponseWriter, r *http.Request) {
	presetID := r.PathValue("id")
	if presetID == "" {
		writeError(w, http.StatusBadRequest, "preset ID is required")
		return
	}

	preset := presets.GetPreset(presetID)
	if preset == nil {
		writeError(w, http.StatusNotFound, "preset not found")
		return
	}

	writeJSON(w, http.StatusOK, preset)
}

func (s *Server) handleGetPresetFields(w http.ResponseWriter, r *http.Request) {
	presetID := r.PathValue("id")
	if presetID == "" {
		presetID = "generic"
	}

	fields := presets.GetDisclosableFields(presetID)
	fixedConstraints := presets.GetFixedConstraints(presetID)

	writeJSON(w, http.StatusOK, map[string]any{
		"presetId":         presetID,
		"fields":           fields,
		"fixedConstraints": fixedConstraints,
	})
}

func (s *Server) handlePetition(w http.ResponseWriter, r *http.Request) {
	petitionID := r.PathValue("id")
	if petitionID == "" {
		http.Error(w, "petition ID required", http.StatusBadRequest)
		return
	}

	var petition *storage.Petition
	var signatures []*storage.Signature
	var totalSignatures int64

	if s.storage != nil {
		var err error
		petition, err = s.storage.GetPetition(r.Context(), petitionID)
		if err != nil {
			s.logger.Error().Err(err).Str("petition_id", petitionID).Msg("failed to get petition")
		}
		if petition != nil {
			signatures, totalSignatures, err = s.storage.GetSignaturesByPetition(r.Context(), petitionID, 10, 0)
			if err != nil {
				s.logger.Error().Err(err).Str("petition_id", petitionID).Msg("failed to get signatures")
			}
		}
	}

	if petition == nil {
		http.Error(w, "petition not found", http.StatusNotFound)
		return
	}

	// Check if the request wants JSON (from the app) or HTML (from browser)
	// Browsers typically send: Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8
	// Apps/programmatic clients send: Accept: application/json or just omit it
	accept := r.Header.Get("Accept")
	format := r.URL.Query().Get("format")
	
	// Return HTML only if explicitly requested (browser behavior)
	// Otherwise default to JSON (app/API behavior)
	wantsHTML := format == "html" || 
		(strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json"))
	
	if !wantsHTML {
		// Return JSON payload for the app
		payload := buildPetitionPayload(petition, baseURL(r))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
		return
	}

	// Return HTML page for browsers
	data := petitionPageData{
		Petition:        petition,
		Signatures:      signatures,
		TotalSignatures: totalSignatures,
		BaseURL:         baseURL(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = petitionPageTemplate.Execute(w, data)
}

func buildPetitionPayload(petition *storage.Petition, base string) map[string]any {
	return map[string]any{
		"kind":         "proof-request",
		"version":      1,
		"aggregateUrl": base + "/api/proofs/aggregate",
		"petitionId":   petition.PetitionID,
		"infoUrl":      base + "/petition/" + petition.PetitionID,
		"service": map[string]any{
			"name":    petition.Name,
			"purpose": petition.Purpose,
			"scope":   petition.Scope,
			"mode":    "fast",
		},
		"query": petition.Query,
	}
}

func (s *Server) handlePetitionQR(w http.ResponseWriter, r *http.Request) {
	petitionID := r.URL.Query().Get("id")
	if petitionID == "" {
		http.Error(w, "petition ID required", http.StatusBadRequest)
		return
	}

	var petition *storage.Petition
	if s.storage != nil {
		var err error
		petition, err = s.storage.GetPetition(r.Context(), petitionID)
		if err != nil || petition == nil {
			http.Error(w, "petition not found", http.StatusNotFound)
			return
		}
	} else {
		http.Error(w, "storage not configured", http.StatusInternalServerError)
		return
	}

	// Build the full JSON payload for the QR code
	payload := buildPetitionPayload(petition, baseURL(r))
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate QR code containing the JSON payload
	png, err := qrcode.Encode(string(jsonBytes), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

type petitionPageData struct {
	Petition        *storage.Petition
	Signatures      []*storage.Signature
	TotalSignatures int64
	BaseURL         string
}

func (s *Server) handleExplorePage(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "date"
	}

	var petitions []*storage.Petition
	var total int64

	if s.storage != nil {
		var err error
		petitions, total, err = s.storage.ListPetitions(r.Context(), 50, 0)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to list petitions")
		}

		if sortBy == "signatures" && len(petitions) > 0 {
			sort.Slice(petitions, func(i, j int) bool {
				return petitions[i].SignatureCount > petitions[j].SignatureCount
			})
		}
	}

	data := explorePageData{
		Petitions: petitions,
		Total:     total,
		SortBy:    sortBy,
		BaseURL:   baseURL(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = explorePageTemplate.Execute(w, data)
}

type explorePageData struct {
	Petitions []*storage.Petition
	Total     int64
	SortBy    string
	BaseURL   string
}

func (s *Server) handleAboutPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = aboutPageTemplate.Execute(w, map[string]string{"BaseURL": baseURL(r)})
}
