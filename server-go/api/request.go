package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

type ProofRequestPayload struct {
	Kind         string         `json:"kind"`
	Version      int            `json:"version"`
	AggregateURL string         `json:"aggregateUrl"`
	PetitionID   string         `json:"petitionId,omitempty"`
	Service      RequestService `json:"service"`
	Query        map[string]any `json:"query,omitempty"`
}

type RequestService struct {
	Name    string `json:"name"`
	Logo    string `json:"logo,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Mode    string `json:"mode,omitempty"`
	DevMode bool   `json:"devMode,omitempty"`
	Domain  string `json:"domain,omitempty"`
}

type requestPageData struct {
	Title               string
	PayloadJSON         string
	PayloadB64          string
	AggregateURL        string
	APKDownloadURL      string
	PetitionLinkBaseURL string
	PetitionViewBaseURL string
	NationalityIn       string
	NationalityOut      string
	IssuingCountryIn    string
	IssuingCountryOut   string
	Disclose            string
	AgeGte              string
	Purpose             string
	Scope               string
	PetitionID          string
	Name                string
}

func petitionDeepLinkURLFromValues(serverBase, deepLinkBase, petitionID string) string {
	upstream, err := url.Parse(strings.TrimSpace(serverBase))
	if err != nil || upstream.Host == "" {
		return joinURL(strings.TrimSpace(deepLinkBase), "/passport")
	}
	payload := upstream.Host + "|" + strings.TrimSpace(petitionID)
	params := url.Values{}
	params.Set("sign", base64.RawURLEncoding.EncodeToString([]byte(payload)))
	return joinURL(strings.TrimSpace(deepLinkBase), "/passport") + "?" + params.Encode()
}

func (s *Server) handleRequestConfig(w http.ResponseWriter, r *http.Request) {
	payload, err := buildPayloadFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleRequestQR(w http.ResponseWriter, r *http.Request) {
	var payloadJSON []byte

	// Check if a pre-built payload is provided as base64
	if payloadB64 := r.URL.Query().Get("payload"); payloadB64 != "" {
		// Decode base64 payload (handle URL-safe base64)
		normalized := strings.ReplaceAll(payloadB64, "-", "+")
		normalized = strings.ReplaceAll(normalized, "_", "/")
		// Add padding if needed
		switch len(normalized) % 4 {
		case 2:
			normalized += "=="
		case 3:
			normalized += "="
		}
		decoded, err := base64.StdEncoding.DecodeString(normalized)
		if err != nil {
			http.Error(w, "invalid base64 payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		payloadJSON = decoded
	} else {
		// Build payload from individual query parameters
		payload, err := buildPayloadFromRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	png, err := qrcode.Encode(requestDeepLinkURL(r, payloadJSON), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

func (s *Server) handleRequestPage(w http.ResponseWriter, r *http.Request) {
	payload, err := buildPayloadFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pretty, _ := json.MarshalIndent(payload, "", "  ")
	data := requestPageData{
		Title:               "Vocdoni Passport Request",
		PayloadJSON:         string(pretty),
		PayloadB64:          base64.RawURLEncoding.EncodeToString(pretty),
		AggregateURL:        payload.AggregateURL,
		APKDownloadURL:      joinURL(baseURL(r), "/downloads/app-release.apk"),
		PetitionLinkBaseURL: appLinkBaseURL(r),
		PetitionViewBaseURL: baseURL(r),
		Purpose:             payload.Service.Purpose,
		Scope:               payload.Service.Scope,
		PetitionID:          payload.PetitionID,
		Name:                payload.Service.Name,
	}
	if payload.Query != nil {
		if v := getNestedStringSlice(payload.Query, "nationality", "in"); len(v) > 0 {
			data.NationalityIn = strings.Join(v, ",")
		}
		if v := getNestedStringSlice(payload.Query, "nationality", "out"); len(v) > 0 {
			data.NationalityOut = strings.Join(v, ",")
		}
		if v := getNestedStringSlice(payload.Query, "issuing_country", "in"); len(v) > 0 {
			data.IssuingCountryIn = strings.Join(v, ",")
		}
		if v := getNestedStringSlice(payload.Query, "issuing_country", "out"); len(v) > 0 {
			data.IssuingCountryOut = strings.Join(v, ",")
		}
		if disclose := collectDiscloseFields(payload.Query); len(disclose) > 0 {
			data.Disclose = strings.Join(disclose, ",")
		}
		if ageGte := getNestedInt(payload.Query, "age", "gte"); ageGte > 0 {
			data.AgeGte = strconv.Itoa(ageGte)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = requestPageTemplate.Execute(w, data)
}

func buildPayloadFromRequest(r *http.Request) (*ProofRequestPayload, error) {
	aggregateURL := strings.TrimSpace(r.URL.Query().Get("aggregateUrl"))
	if aggregateURL == "" {
		aggregateURL = joinURL(baseURL(r), "/api/proofs/aggregate")
	}
	name := firstNonEmpty(r.URL.Query().Get("name"), "Vocdoni Passport")
	purpose := firstNonEmpty(r.URL.Query().Get("purpose"), "Generate a passport proof for this service")
	scope := firstNonEmpty(r.URL.Query().Get("scope"), "vocdoni-passport")
	mode := firstNonEmpty(r.URL.Query().Get("mode"), "fast")
	petitionID := strings.TrimSpace(r.URL.Query().Get("petitionId"))
	payload := &ProofRequestPayload{
		Kind:         "vocdoni-passport-request",
		Version:      1,
		AggregateURL: aggregateURL,
		PetitionID:   petitionID,
		Service: RequestService{
			Name:    name,
			Purpose: purpose,
			Scope:   scope,
			Mode:    mode,
			DevMode: strings.EqualFold(r.URL.Query().Get("devMode"), "true"),
			Domain:  publicHost(r),
		},
	}
	query := map[string]any{}
	if age := parseIntParam(r, "ageGte"); age > 0 {
		query["age"] = map[string]any{"gte": age}
	}
	if vals := parseCSVUpper(r.URL.Query().Get("nationalityIn")); len(vals) > 0 {
		query["nationality"] = mergeQueryMap(query["nationality"], map[string]any{"in": vals})
	}
	if vals := parseCSVUpper(r.URL.Query().Get("nationalityOut")); len(vals) > 0 {
		query["nationality"] = mergeQueryMap(query["nationality"], map[string]any{"out": vals})
	}
	if vals := parseCSVUpper(r.URL.Query().Get("issuingCountryIn")); len(vals) > 0 {
		query["issuing_country"] = mergeQueryMap(query["issuing_country"], map[string]any{"in": vals})
	}
	if vals := parseCSVUpper(r.URL.Query().Get("issuingCountryOut")); len(vals) > 0 {
		query["issuing_country"] = mergeQueryMap(query["issuing_country"], map[string]any{"out": vals})
	}
	for _, field := range parseCSV(r.URL.Query().Get("disclose")) {
		query[field] = mergeQueryMap(query[field], map[string]any{"disclose": true})
	}
	if len(query) > 0 {
		payload.Query = query
	}
	return payload, nil
}

func mergeQueryMap(existing any, extra map[string]any) map[string]any {
	out := map[string]any{}
	if m, ok := existing.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func parseCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseCSVUpper(s string) []string {
	parts := parseCSV(s)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.ToUpper(p))
	}
	return out
}

func parseIntParam(r *http.Request, key string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	return v
}

func firstNonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func baseURL(r *http.Request) string {
	if configured := configuredPublicBaseURL(); configured != "" {
		return configured
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func configuredPublicBaseURL() string {
	value := strings.TrimSpace(os.Getenv("VOCDONI_PUBLIC_BASE_URL"))
	return strings.TrimRight(value, "/")
}

func configuredDeepLinkBaseURL() string {
	value := strings.TrimSpace(os.Getenv("VOCDONI_DEEPLINK_BASE_URL"))
	return strings.TrimRight(value, "/")
}

func publicHost(r *http.Request) string {
	if configured := configuredPublicBaseURL(); configured != "" {
		parsed, err := url.Parse(configured)
		if err == nil && parsed.Host != "" {
			return hostOnly(parsed.Host)
		}
	}
	return hostOnly(r.Host)
}

func appLinkBaseURL(r *http.Request) string {
	if configured := configuredDeepLinkBaseURL(); configured != "" {
		return configured
	}
	return baseURL(r)
}

func joinURL(base, path string) string {
	if strings.TrimSpace(base) == "" {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func hostOnly(hostport string) string {
	if i := strings.Index(hostport, ":"); i >= 0 {
		return hostport[:i]
	}
	return hostport
}

func getNestedStringSlice(m map[string]any, outer, inner string) []string {
	child, ok := m[outer].(map[string]any)
	if !ok {
		return nil
	}
	arr, ok := child[inner].([]string)
	if ok {
		return arr
	}
	generic, ok := child[inner].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(generic))
	for _, v := range generic {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func getNestedInt(m map[string]any, outer, inner string) int {
	child, ok := m[outer].(map[string]any)
	if !ok {
		return 0
	}
	switch v := child[inner].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func collectDiscloseFields(m map[string]any) []string {
	out := []string{}
	for k, raw := range m {
		child, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if disclose, ok := child["disclose"].(bool); ok && disclose {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

var requestPageTemplate = template.Must(template.New("request-page").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{.Title}}</title>
  <style>
    * { box-sizing: border-box; }
    body { font-family: system-ui, -apple-system, sans-serif; background:#f3f4f6; color:#111827; margin:0; line-height:1.5; }
    .nav { background: #1f2937; padding: 0 20px; }
    .nav-inner { max-width: 1200px; margin: 0 auto; display: flex; align-items: center; justify-content: space-between; }
    .nav-brand { display: flex; align-items: center; gap: 10px; color: #fff; font-weight: 700; font-size: 18px; text-decoration: none; padding: 16px 0; }
    .nav-brand:hover { color: #e0e7ff; }
    .nav-links { display: flex; gap: 4px; }
    .nav-link { color: #d1d5db; text-decoration: none; padding: 10px 16px; border-radius: 6px; font-size: 14px; font-weight: 500; transition: all 0.15s; }
    .nav-link:hover { color: #fff; background: rgba(255,255,255,0.1); }
    .nav-link.active { color: #fff; background: #4f46e5; }
    .wrap { max-width: 1200px; margin: 0 auto; padding: 20px; }
    .grid { display:grid; grid-template-columns: 1fr 380px; gap: 20px; }
    .card { background:#fff; border-radius:12px; padding:20px; box-shadow:0 1px 3px rgba(0,0,0,.08); margin-bottom:16px; }
    h1 { margin:0 0 8px 0; font-size: 24px; }
    h2 { margin:0 0 16px 0; font-size: 18px; font-weight: 700; }
    h3 { margin:16px 0 8px 0; font-size: 13px; font-weight: 700; color: #6b7280; text-transform: uppercase; letter-spacing: 0.5px; }
    label { display:block; font-weight:600; font-size: 13px; margin:12px 0 4px 0; color: #374151; }
    input[type="text"], input[type="number"], textarea, select { width:100%; padding:10px 12px; border-radius:8px; border:1px solid #d1d5db; font-size:14px; transition: border-color 0.15s; }
    input:focus, select:focus, textarea:focus { outline: none; border-color: #6366f1; box-shadow: 0 0 0 3px rgba(99,102,241,0.1); }
    textarea { min-height: 120px; font-family: ui-monospace, monospace; font-size: 12px; resize: vertical; }
    select { cursor: pointer; background: #fff url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%236b7280' d='M2 4l4 4 4-4'/%3E%3C/svg%3E") no-repeat right 12px center; appearance: none; padding-right: 36px; }
    .muted { color:#6b7280; font-size:13px; }
    .small { font-size: 12px; }
    .qr-section { text-align:center; }
    .qr-section img { width: 220px; height: 220px; border-radius:12px; background:#fff; border: 1px solid #e5e7eb; }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; }
    .chips span { background:#eef2ff; color:#4338ca; border-radius:6px; padding:4px 10px; font-size:12px; font-weight: 500; }
    .chips span.constraint { background:#fef3c7; color:#92400e; }
    .btn { display:inline-flex; align-items:center; justify-content:center; padding:10px 16px; border-radius:8px; font-size:14px; font-weight:600; border:none; cursor:pointer; transition: all 0.15s; text-decoration: none; }
    .btn-primary { background:#4f46e5; color:#fff; }
    .btn-primary:hover { background:#4338ca; }
    .btn-secondary { background:#f3f4f6; color:#374151; border: 1px solid #d1d5db; }
    .btn-secondary:hover { background:#e5e7eb; }
    .btn-success { background:#059669; color:#fff; }
    .btn-success:hover { background:#047857; }
    .btn-sm { padding: 6px 12px; font-size: 13px; }
    .btn-group { display:flex; gap:8px; flex-wrap: wrap; }
    .field-grid { display:grid; grid-template-columns: repeat(4, 1fr); gap: 6px; }
    .field-item { position: relative; display:flex; align-items:center; gap:6px; padding:6px 8px; background:#f9fafb; border-radius:6px; cursor:pointer; transition: all 0.15s; border: 1px solid transparent; font-size: 12px; }
    .field-item:hover { background:#f3f4f6; border-color: #d1d5db; }
    .field-item input { width:14px; height:14px; cursor:pointer; margin: 0; }
    .field-item span { color:#374151; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .field-item.checked { background:#eef2ff; border-color: #c7d2fe; }
    .field-item.checked span { color:#4338ca; font-weight:500; }
    .field-item.disabled { opacity: 0.5; cursor: not-allowed; }
    .field-item .tooltip { display: none; position: absolute; bottom: 100%; left: 50%; transform: translateX(-50%); background: #1f2937; color: #fff; padding: 6px 10px; border-radius: 6px; font-size: 11px; white-space: nowrap; z-index: 100; margin-bottom: 4px; }
    .field-item .tooltip::after { content: ''; position: absolute; top: 100%; left: 50%; transform: translateX(-50%); border: 5px solid transparent; border-top-color: #1f2937; }
    .field-item:hover .tooltip { display: block; }
    .section { border-top: 1px solid #e5e7eb; margin-top: 16px; padding-top: 16px; }
    .status-msg { padding: 10px 14px; border-radius: 8px; font-size: 13px; margin-top: 12px; }
    .status-msg.success { background: #d1fae5; color: #065f46; }
    .status-msg.error { background: #fee2e2; color: #991b1b; }
    .status-msg.info { background: #dbeafe; color: #1e40af; }
    .petition-info { background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 8px; padding: 12px; margin-top: 12px; }
    .petition-info h4 { margin: 0 0 8px 0; font-size: 14px; color: #166534; }
    .petition-info .stat { display: flex; justify-content: space-between; font-size: 13px; padding: 4px 0; }
    .petition-info .stat-value { font-weight: 600; color: #166534; }
    .input-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    .preset-badge { display: inline-block; background: #dbeafe; color: #1e40af; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 600; margin-left: 8px; }
    @media (max-width: 900px) { .grid { grid-template-columns: 1fr; } .field-grid { grid-template-columns: repeat(2, 1fr); } .input-row { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <nav class="nav">
    <div class="nav-inner">
      <a href="/" class="nav-brand">🛂 Vocdoni Passport</a>
      <div class="nav-links">
        <a href="/" class="nav-link active">Create</a>
        <a href="/explore" class="nav-link">Explore</a>
        <a href="/about" class="nav-link">About</a>
      </div>
    </div>
  </nav>
  <div class="wrap">
    <div class="card">
      <h1>📝 Create Petition</h1>
      <p class="muted">Create a signing petition with specific requirements. Users scan the QR code with the mobile app to sign.</p>
    </div>
    <div class="grid">
      <div>
        <div class="card">
          <h2>📝 Petition Configuration</h2>
          
          <label>Document Preset</label>
          <select id="preset" onchange="loadPreset()">
            <option value="generic">Generic (All Countries)</option>
            <option value="esp_dni">🇪🇸 Spanish DNI</option>
            <option value="deu_personalausweis">🇩🇪 German Personalausweis</option>
            <option value="fra_cni">🇫🇷 French CNI</option>
            <option value="eu_passport">🇪🇺 EU Passport</option>
          </select>
          <p class="muted small" id="presetDescription">Standard fields available in most ICAO 9303 compliant documents</p>

          <div class="input-row">
            <div>
              <label>Petition Name</label>
              <input type="text" id="name" value="{{.Name}}" placeholder="My Voting Petition" />
            </div>
            <div>
              <label>Scope</label>
              <input type="text" id="scope" value="{{.Scope}}" placeholder="vocdoni-passport" />
            </div>
          </div>
          
          <label>Purpose</label>
          <input type="text" id="purpose" value="{{.Purpose}}" placeholder="Describe why signatures are needed" />

          <div class="section">
            <h3>📋 Required Disclosures</h3>
            <p class="muted small">Select fields that signers must reveal. Hover for examples.</p>
            <div class="field-grid" id="disclosureFields"></div>
          </div>

          <div class="section">
            <h3>🔒 Eligibility Requirements</h3>
            <div class="input-row">
              <div>
                <label>Nationality (include)</label>
                <input type="text" id="nationalityIn" value="{{.NationalityIn}}" placeholder="ESP,FRA,DEU" />
              </div>
              <div>
                <label>Nationality (exclude)</label>
                <input type="text" id="nationalityOut" value="{{.NationalityOut}}" placeholder="Leave empty" />
              </div>
            </div>
            <div class="input-row">
              <div>
                <label>Issuing Country (include)</label>
                <input type="text" id="issuingCountryIn" value="{{.IssuingCountryIn}}" placeholder="ESP,FRA" />
              </div>
              <div>
                <label>Minimum Age</label>
                <input type="number" id="ageGte" value="{{.AgeGte}}" placeholder="18" min="0" max="150" />
              </div>
            </div>
          </div>

          <div class="section">
            <div class="btn-group">
              <button class="btn btn-success" type="button" onclick="createPetition()">🚀 Create Petition</button>
            </div>
            <div id="petitionStatus"></div>
            <div id="petitionInfo" style="display:none"></div>
          </div>
        </div>

        <div class="card" id="payloadPreview">
          <h2>📄 Payload Preview</h2>
          <textarea id="payload" readonly style="min-height:80px;">{{.PayloadJSON}}</textarea>
          <p class="muted small">This is a preview. Create the petition to get the QR code.</p>
        </div>
      </div>
      
      <div>
        <div class="card qr-section" id="qrSection" style="text-align:center;">
          <h2>📱 Scan to Sign</h2>
          <div id="qrPlaceholder" style="width:220px;height:220px;margin:0 auto;background:#f3f4f6;border-radius:12px;display:flex;align-items:center;justify-content:center;border:2px dashed #d1d5db;">
            <div style="text-align:center;color:#9ca3af;">
              <div style="font-size:48px;margin-bottom:8px;">📝</div>
              <div style="font-size:13px;">Create petition first</div>
            </div>
          </div>
          <img id="qr" src="" alt="QR code" style="display:none;margin:0 auto;" />
          <p class="muted small" id="qrHint">Configure your petition and click "Create Petition"</p>
          <div id="qrActions" style="display:none;">
            <div class="btn-group" style="justify-content:center; margin-top:12px;">
              <button class="btn btn-secondary btn-sm" type="button" onclick="copyPetitionLink()">📋 Copy Link</button>
              <a class="btn btn-primary btn-sm" id="viewPetitionBtn" href="#" target="_blank">View Petition →</a>
            </div>
          </div>
          <div class="btn-group" style="justify-content:center; margin-top:12px;">
            <a class="btn btn-secondary btn-sm" href="{{.APKDownloadURL}}">📥 Download APK</a>
          </div>
        </div>

        <div class="card">
          <h2>✅ Active Requirements</h2>
          <div class="chips" id="chips"><span>No constraints</span></div>
        </div>

        <input type="hidden" id="aggregateUrl" value="{{.AggregateURL}}" />
        <input type="hidden" id="petitionId" value="{{.PetitionID}}" />
        <input type="hidden" id="petitionLinkBaseUrl" value="{{.PetitionLinkBaseURL}}" />
        <input type="hidden" id="petitionViewBaseUrl" value="{{.PetitionViewBaseURL}}" />
      </div>
    </div>
  </div>
<script>
const PRESETS = {
  generic: {
    name: 'Generic (All Countries)',
    description: 'Standard MRZ fields available in all ICAO 9303 documents',
    fields: [
      { id: 'nationality', label: 'Nationality', example: 'ESP, FRA, DEU' },
      { id: 'issuing_country', label: 'Issuing Country', example: 'Country that issued the document' },
      { id: 'firstname', label: 'First Name', example: 'JUAN' },
      { id: 'lastname', label: 'Last Name', example: 'GARCIA LOPEZ' },
      { id: 'fullname', label: 'Full Name', example: 'GARCIA LOPEZ, JUAN' },
      { id: 'birthdate', label: 'Date of Birth', example: '850315 (YYMMDD)' },
      { id: 'expiry_date', label: 'Expiry Date', example: '280315 (YYMMDD)' },
      { id: 'document_number', label: 'Document Number', example: 'AB1234567' },
      { id: 'document_type', label: 'Document Type', example: 'P (Passport), ID (Card)' },
      { id: 'gender', label: 'Gender', example: 'M, F, X' },
      { id: 'optional_data_1', label: 'Optional Data 1', example: 'Country-specific field' },
      { id: 'optional_data_2', label: 'Optional Data 2', example: 'ID cards only (TD1)' },
    ],
    fixedConstraints: {}
  },
  esp_dni: {
    name: 'Spanish DNI',
    description: 'Spanish National ID Card (Documento Nacional de Identidad)',
    fields: [
      { id: 'nationality', label: 'Nacionalidad', example: 'ESP (fijo)', fixed: true },
      { id: 'issuing_country', label: 'País Emisor', example: 'ESP (fijo)', fixed: true },
      { id: 'firstname', label: 'Nombre', example: 'JUAN CARLOS' },
      { id: 'lastname', label: 'Apellidos', example: 'GARCIA LOPEZ' },
      { id: 'fullname', label: 'Nombre Completo', example: 'GARCIA LOPEZ, JUAN CARLOS' },
      { id: 'birthdate', label: 'Fecha Nacimiento', example: '850315' },
      { id: 'expiry_date', label: 'Fecha Caducidad', example: '280315' },
      { id: 'document_number', label: 'Nº Soporte', example: 'AAA123456' },
      { id: 'document_type', label: 'Tipo Documento', example: 'ID' },
      { id: 'gender', label: 'Sexo', example: 'M, F' },
      { id: 'optional_data_1', label: 'Número DNI', example: '12345678Z' },
    ],
    fixedConstraints: { nationality: { in: ['ESP'] }, issuing_country: { in: ['ESP'] } }
  },
  deu_personalausweis: {
    name: 'German Personalausweis',
    description: 'German National ID Card',
    fields: [
      { id: 'nationality', label: 'Nationality', example: 'D/DEU', fixed: true },
      { id: 'issuing_country', label: 'Issuing Country', example: 'D/DEU', fixed: true },
      { id: 'firstname', label: 'Vorname', example: 'HANS' },
      { id: 'lastname', label: 'Nachname', example: 'MUELLER' },
      { id: 'fullname', label: 'Vollständiger Name', example: 'MUELLER, HANS' },
      { id: 'birthdate', label: 'Geburtsdatum', example: '850315' },
      { id: 'expiry_date', label: 'Gültig bis', example: '280315' },
      { id: 'document_number', label: 'Ausweisnummer', example: 'T220001293' },
      { id: 'gender', label: 'Geschlecht', example: 'M, F' },
    ],
    fixedConstraints: { nationality: { in: ['D', 'DEU'] }, issuing_country: { in: ['D', 'DEU'] } }
  },
  fra_cni: {
    name: 'French CNI',
    description: 'French National ID Card (Carte Nationale d\'Identité)',
    fields: [
      { id: 'nationality', label: 'Nationalité', example: 'FRA', fixed: true },
      { id: 'issuing_country', label: 'Pays émetteur', example: 'FRA', fixed: true },
      { id: 'firstname', label: 'Prénom(s)', example: 'JEAN PIERRE' },
      { id: 'lastname', label: 'Nom', example: 'DUPONT' },
      { id: 'fullname', label: 'Nom complet', example: 'DUPONT, JEAN PIERRE' },
      { id: 'birthdate', label: 'Date naissance', example: '850315' },
      { id: 'expiry_date', label: 'Date expiration', example: '280315' },
      { id: 'document_number', label: 'Numéro carte', example: '123456789012' },
      { id: 'gender', label: 'Sexe', example: 'M, F' },
    ],
    fixedConstraints: { nationality: { in: ['FRA'] }, issuing_country: { in: ['FRA'] } }
  },
  eu_passport: {
    name: 'EU Passport',
    description: 'European Union Member State Passports',
    fields: [
      { id: 'nationality', label: 'Nationality', example: 'Any EU country' },
      { id: 'issuing_country', label: 'Issuing Country', example: 'Any EU country' },
      { id: 'firstname', label: 'First Name', example: 'JUAN' },
      { id: 'lastname', label: 'Last Name', example: 'GARCIA' },
      { id: 'fullname', label: 'Full Name', example: 'GARCIA, JUAN' },
      { id: 'birthdate', label: 'Date of Birth', example: '850315' },
      { id: 'expiry_date', label: 'Expiry Date', example: '280315' },
      { id: 'document_number', label: 'Passport Number', example: 'AB1234567' },
      { id: 'gender', label: 'Gender', example: 'M, F, X' },
      { id: 'optional_data_1', label: 'Personal Number', example: 'National ID if present' },
    ],
    fixedConstraints: {}
  }
};

let currentPreset = 'generic';
let selectedDisclosures = new Set();

function loadPreset() {
  const presetId = document.getElementById('preset').value;
  currentPreset = presetId;
  const preset = PRESETS[presetId];
  
  document.getElementById('presetDescription').textContent = preset.description;
  
  // Apply fixed constraints
  if (preset.fixedConstraints.nationality?.in) {
    document.getElementById('nationalityIn').value = preset.fixedConstraints.nationality.in.join(',');
    document.getElementById('nationalityIn').disabled = true;
  } else {
    document.getElementById('nationalityIn').disabled = false;
  }
  if (preset.fixedConstraints.issuing_country?.in) {
    document.getElementById('issuingCountryIn').value = preset.fixedConstraints.issuing_country.in.join(',');
    document.getElementById('issuingCountryIn').disabled = true;
  } else {
    document.getElementById('issuingCountryIn').disabled = false;
  }
  
  // Rebuild field checkboxes
  renderDisclosureFields(preset);
  render();
}

function renderDisclosureFields(preset) {
  const container = document.getElementById('disclosureFields');
  container.innerHTML = '';
  
  for (const field of preset.fields) {
    const item = document.createElement('label');
    item.className = 'field-item' + (selectedDisclosures.has(field.id) ? ' checked' : '') + (field.fixed ? ' disabled' : '');
    
    const checkbox = document.createElement('input');
    checkbox.type = 'checkbox';
    checkbox.name = 'disclose';
    checkbox.value = field.id;
    checkbox.checked = selectedDisclosures.has(field.id);
    checkbox.disabled = field.fixed;
    checkbox.addEventListener('change', function() {
      if (this.checked) {
        selectedDisclosures.add(field.id);
      } else {
        selectedDisclosures.delete(field.id);
      }
      item.classList.toggle('checked', this.checked);
      render();
    });
    
    const label = document.createElement('span');
    label.textContent = field.label;
    
    const tooltip = document.createElement('div');
    tooltip.className = 'tooltip';
    tooltip.textContent = field.example;
    
    item.appendChild(checkbox);
    item.appendChild(label);
    item.appendChild(tooltip);
    container.appendChild(item);
  }
}

function splitCsv(v) { return v.split(',').map(x => x.trim()).filter(Boolean); }

function getSelectedDisclosures() {
  return Array.from(selectedDisclosures);
}

function encodeBase64Url(value) {
  return btoa(value).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function buildPetitionDeepLinkUrl(petitionId) {
  const petitionLinkBaseUrl = document.getElementById('petitionLinkBaseUrl').value.trim() || window.location.origin;
  const petitionViewBaseUrl = document.getElementById('petitionViewBaseUrl').value.trim() || window.location.origin;

  let serverHost;
  try {
    serverHost = new URL(petitionViewBaseUrl).host;
  } catch {
    serverHost = petitionViewBaseUrl.replace(/^https?:\/\//, '').replace(/\/.*$/, '');
  }

  const sign = encodeBase64Url(serverHost + '|' + petitionId);
  return petitionLinkBaseUrl.replace(/\/$/, '') + '/passport?sign=' + encodeURIComponent(sign);
}

function buildPayload() {
  const payload = {
    kind: 'vocdoni-passport-request',
    version: 1,
    aggregateUrl: document.getElementById('aggregateUrl').value.trim(),
    petitionId: document.getElementById('petitionId').value.trim() || undefined,
    service: {
      name: document.getElementById('name').value.trim() || 'Vocdoni Passport',
      purpose: document.getElementById('purpose').value.trim(),
      scope: document.getElementById('scope').value.trim(),
      mode: 'fast'
    }
  };
  
  const query = {};
  const preset = PRESETS[currentPreset];
  
  // Apply fixed constraints from preset
  if (preset.fixedConstraints.nationality) {
    query.nationality = { ...preset.fixedConstraints.nationality };
  }
  if (preset.fixedConstraints.issuing_country) {
    query.issuing_country = { ...preset.fixedConstraints.issuing_country };
  }
  
  // User-configured constraints
  const inVals = splitCsv(document.getElementById('nationalityIn').value.toUpperCase());
  const outVals = splitCsv(document.getElementById('nationalityOut').value.toUpperCase());
  const issuingInVals = splitCsv(document.getElementById('issuingCountryIn').value.toUpperCase());
  const age = parseInt(document.getElementById('ageGte').value.trim(), 10);
  
  if (!preset.fixedConstraints.nationality && (inVals.length || outVals.length)) {
    query.nationality = query.nationality || {};
    if (inVals.length) query.nationality.in = inVals;
    if (outVals.length) query.nationality.out = outVals;
  }
  if (!preset.fixedConstraints.issuing_country && issuingInVals.length) {
    query.issuing_country = query.issuing_country || {};
    query.issuing_country.in = issuingInVals;
  }
  if (!Number.isNaN(age) && age > 0) query.age = { gte: age };
  
  // Disclosures
  for (const f of getSelectedDisclosures()) {
    query[f] = { disclose: true };
  }
  
  if (Object.keys(query).length) payload.query = query;
  return payload;
}

function render() {
  const payload = buildPayload();
  const json = JSON.stringify(payload, null, 2);
  document.getElementById('payload').value = json;
  
  const petitionId = document.getElementById('petitionId').value.trim();
  const petitionViewBaseUrl = document.getElementById('petitionViewBaseUrl').value.trim() || window.location.origin;
  if (petitionId) {
    // Petition created - show QR code
    document.getElementById('qrPlaceholder').style.display = 'none';
    document.getElementById('qr').style.display = 'block';
    document.getElementById('qr').src = '/api/petition-qr.png?id=' + encodeURIComponent(petitionId);
    document.getElementById('qrHint').textContent = 'Scan with Vocdoni Passport app to sign';
    document.getElementById('qrActions').style.display = 'block';
    document.getElementById('viewPetitionBtn').href = petitionViewBaseUrl.replace(/\/$/, '') + '/petition/' + encodeURIComponent(petitionId);
  } else {
    // No petition created yet - show placeholder
    document.getElementById('qrPlaceholder').style.display = 'flex';
    document.getElementById('qr').style.display = 'none';
    document.getElementById('qrHint').textContent = 'Configure your petition and click "Create Petition"';
    document.getElementById('qrActions').style.display = 'none';
  }
  
  // Render chips
  const chips = [];
  const preset = PRESETS[currentPreset];
  
  if (payload.query?.nationality?.in) chips.push({ text: 'Nationality: ' + payload.query.nationality.in.join(', '), type: 'constraint' });
  if (payload.query?.nationality?.out) chips.push({ text: 'Nationality ≠ ' + payload.query.nationality.out.join(', '), type: 'constraint' });
  if (payload.query?.issuing_country?.in) chips.push({ text: 'Issuing: ' + payload.query.issuing_country.in.join(', '), type: 'constraint' });
  if (payload.query?.age?.gte) chips.push({ text: 'Age ≥ ' + payload.query.age.gte, type: 'constraint' });
  
  for (const [k, v] of Object.entries(payload.query || {})) {
    if (v && v.disclose) {
      const field = preset.fields.find(f => f.id === k);
      chips.push({ text: field?.label || k, type: '' });
    }
  }
  
  const chipsHtml = chips.length 
    ? chips.map(c => '<span class="' + c.type + '">' + c.text + '</span>').join('')
    : '<span>No constraints</span>';
  document.getElementById('chips').innerHTML = chipsHtml;
}

async function copyPetitionLink() {
  const petitionId = document.getElementById('petitionId').value.trim();
  if (!petitionId) {
    showStatus('error', '❌ Create a petition first');
    return;
  }
  const url = buildPetitionDeepLinkUrl(petitionId);
  try {
    await navigator.clipboard.writeText(url);
    showStatus('info', '📋 Petition link copied to clipboard!');
  } catch {
    showStatus('error', '❌ Failed to copy link');
  }
}

function showStatus(type, message) {
  const statusEl = document.getElementById('petitionStatus');
  statusEl.innerHTML = '<div class="status-msg ' + type + '">' + message + '</div>';
  if (type !== 'error') {
    setTimeout(() => { statusEl.innerHTML = ''; }, 5000);
  }
}

async function createPetition() {
  const statusEl = document.getElementById('petitionStatus');
  statusEl.innerHTML = '<div class="status-msg info">Creating petition...</div>';
  
  const payload = buildPayload();
  const petitionData = {
    name: payload.service?.name || 'Vocdoni Passport',
    purpose: payload.service?.purpose || '',
    scope: payload.service?.scope || '',
    query: payload.query || {},
    preset: currentPreset
  };

  try {
    const resp = await fetch('/api/petitions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(petitionData)
    });
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || 'Failed to create petition');
    
    document.getElementById('petitionId').value = data.petitionId;
    render();
    
    showStatus('success', '✅ Petition created successfully!');
    showPetitionInfo(data);
  } catch (err) {
    showStatus('error', '❌ ' + err.message);
  }
}

function showPetitionInfo(petition) {
  const infoEl = document.getElementById('petitionInfo');
  infoEl.style.display = 'block';
  infoEl.innerHTML = '<div class="petition-info">' +
    '<h4>✅ Petition Created</h4>' +
    '<div class="stat"><span>ID:</span><span class="stat-value" style="font-family:monospace;font-size:12px;">' + petition.petitionId + '</span></div>' +
    '<div class="stat"><span>Name:</span><span class="stat-value">' + petition.name + '</span></div>' +
    '</div>';
}

// Initialize
const urlParams = new URLSearchParams(window.location.search);
if (urlParams.get('preset')) {
  document.getElementById('preset').value = urlParams.get('preset');
}
if (urlParams.get('disclose')) {
  urlParams.get('disclose').split(',').forEach(f => selectedDisclosures.add(f));
}

loadPreset();

// Event listeners
for (const id of ['name','purpose','scope','nationalityIn','nationalityOut','issuingCountryIn','ageGte']) {
  document.getElementById(id).addEventListener('input', render);
}
</script>
</body>
</html>`))

var petitionPageTemplate = template.Must(template.New("petition-page").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{.Petition.Name}} - Vocdoni Passport</title>
  <style>
    * { box-sizing: border-box; }
    body { font-family: system-ui, -apple-system, sans-serif; background:#f3f4f6; color:#111827; margin:0; line-height:1.5; }
    .nav { background: #1f2937; padding: 0 20px; }
    .nav-inner { max-width: 900px; margin: 0 auto; display: flex; align-items: center; justify-content: space-between; }
    .nav-brand { display: flex; align-items: center; gap: 10px; color: #fff; font-weight: 700; font-size: 18px; text-decoration: none; padding: 16px 0; }
    .nav-brand:hover { color: #e0e7ff; }
    .nav-links { display: flex; gap: 4px; }
    .nav-link { color: #d1d5db; text-decoration: none; padding: 10px 16px; border-radius: 6px; font-size: 14px; font-weight: 500; transition: all 0.15s; }
    .nav-link:hover { color: #fff; background: rgba(255,255,255,0.1); }
    .nav-link.active { color: #fff; background: #4f46e5; }
    .wrap { max-width: 900px; margin: 0 auto; padding: 20px; }
    .card { background:#fff; border-radius:12px; padding:24px; box-shadow:0 1px 3px rgba(0,0,0,.08); margin-bottom:20px; }
    h1 { margin:0 0 8px 0; font-size: 28px; }
    h2 { margin:0 0 16px 0; font-size: 20px; font-weight: 700; }
    .muted { color:#6b7280; font-size:14px; }
    .stat-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin: 20px 0; }
    .stat-card { background: #f9fafb; border-radius: 12px; padding: 20px; text-align: center; }
    .stat-value { font-size: 36px; font-weight: 800; color: #4f46e5; }
    .stat-label { font-size: 13px; color: #6b7280; margin-top: 4px; }
    .chips { display: flex; flex-wrap: wrap; gap: 8px; margin: 16px 0; }
    .chips span { background:#eef2ff; color:#4338ca; border-radius:6px; padding:6px 12px; font-size:13px; font-weight: 500; }
    .chips span.constraint { background:#fef3c7; color:#92400e; }
    .qr-section { text-align: center; padding: 20px; }
    .qr-section img { width: 200px; height: 200px; border-radius: 12px; border: 1px solid #e5e7eb; }
    .btn { display:inline-flex; align-items:center; justify-content:center; padding:10px 16px; border-radius:8px; font-size:14px; font-weight:600; border:none; cursor:pointer; text-decoration: none; }
    .btn-primary { background:#4f46e5; color:#fff; }
    .btn-secondary { background:#f3f4f6; color:#374151; border: 1px solid #d1d5db; }
    .signature-list { margin-top: 16px; }
    .signature-item { display: flex; justify-content: space-between; align-items: center; padding: 12px 16px; background: #f9fafb; border-radius: 8px; margin-bottom: 8px; font-size: 13px; flex-wrap: wrap; gap: 8px; }
    .signature-item .info { display: flex; flex-direction: column; gap: 4px; flex: 1; min-width: 0; }
    .signature-item .nullifier { font-family: monospace; color: #6b7280; overflow: hidden; text-overflow: ellipsis; font-size: 12px; }
    .signature-item .address { font-family: monospace; color: #4f46e5; font-size: 12px; font-weight: 500; }
    .signature-item .time { color: #9ca3af; white-space: nowrap; }
    .empty-state { text-align: center; padding: 40px; color: #9ca3af; }
    .info-row { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #f3f4f6; }
    .info-row:last-child { border-bottom: none; }
    .info-label { color: #6b7280; font-size: 13px; }
    .info-value { font-weight: 600; font-size: 13px; }
    @media (max-width: 600px) { .stat-grid { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <nav class="nav">
    <div class="nav-inner">
      <a href="/" class="nav-brand">🛂 Vocdoni Passport</a>
      <div class="nav-links">
        <a href="/" class="nav-link">Create</a>
        <a href="/explore" class="nav-link">Explore</a>
        <a href="/about" class="nav-link">About</a>
      </div>
    </div>
  </nav>
  <div class="wrap">
    <div class="card">
      <h1>📋 {{.Petition.Name}}</h1>
      <p class="muted">{{.Petition.Purpose}}</p>
      
      <div class="stat-grid">
        <div class="stat-card">
          <div class="stat-value">{{.Petition.SignatureCount}}</div>
          <div class="stat-label">Total Signatures</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{len .Petition.DisclosedFields}}</div>
          <div class="stat-label">Disclosed Fields</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">✓</div>
          <div class="stat-label">Active</div>
        </div>
      </div>

      <h3 style="margin:20px 0 8px 0; font-size:14px; color:#6b7280;">Required Disclosures</h3>
      <div class="chips">
        {{range .Petition.DisclosedFields}}<span>{{.}}</span>{{end}}
        {{if not .Petition.DisclosedFields}}<span style="background:#f3f4f6;color:#9ca3af;">None required</span>{{end}}
      </div>
    </div>

    <div class="card qr-section" style="text-align:center;">
      <h2>📱 Scan to Sign</h2>
      <img id="qr" src="/api/petition-qr.png?id={{.Petition.PetitionID}}" alt="QR code" style="margin:0 auto;" />
      <p class="muted">Open the Vocdoni Passport app and scan this QR code to sign</p>
      <div class="btn-group" style="justify-content:center; margin-top:16px; gap:8px;">
        <button class="btn btn-secondary" type="button" onclick="copyLink(this)">📋 Copy Link</button>
        <a class="btn btn-primary" href="{{.BaseURL}}/downloads/app-release.apk">📥 Download App</a>
      </div>
      <input type="hidden" id="deepLinkUrl" value="{{.DeepLinkURL}}" />
    </div>
    <script>
    function copyLink(button) {
      const url = document.getElementById('deepLinkUrl').value;
      navigator.clipboard.writeText(url).then(() => {
        const original = button.textContent;
        button.textContent = '✓ Copied!';
        setTimeout(() => { button.textContent = original; }, 2000);
      }).catch(() => {
        alert('Failed to copy link');
      });
    }
    </script>

    <div class="card">
      <h2>📋 Petition Details</h2>
      <div class="info-row">
        <span class="info-label">Petition ID</span>
        <span class="info-value" style="font-family:monospace;">{{.Petition.PetitionID}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">Scope</span>
        <span class="info-value">{{.Petition.Scope}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">Created</span>
        <span class="info-value">{{.Petition.CreatedAt.Format "Jan 2, 2006 15:04"}}</span>
      </div>
      {{if .Petition.Preset}}
      <div class="info-row">
        <span class="info-label">Document Preset</span>
        <span class="info-value">{{.Petition.Preset}}</span>
      </div>
      {{end}}
    </div>

    <div class="card">
      <h2>📝 Recent Signatures ({{.TotalSignatures}} total)</h2>
      {{if .Signatures}}
      <div class="signature-list">
        {{range .Signatures}}
        <div class="signature-item">
          <div class="info">
            {{if .SignerAddress}}<span class="address" title="Signer Address">{{.SignerAddress}}</span>{{end}}
            <span class="nullifier" title="Nullifier: {{.Nullifier}}">{{.Nullifier}}</span>
          </div>
          <span class="time">{{.Timestamp.Format "Jan 2, 15:04"}}</span>
        </div>
        {{end}}
      </div>
      {{if gt .TotalSignatures 10}}
      <p class="muted" style="text-align:center;margin-top:16px;">Showing 10 of {{.TotalSignatures}} signatures</p>
      {{end}}
      {{else}}
      <div class="empty-state">
        <p>📭 No signatures yet</p>
        <p class="muted">Be the first to sign this petition!</p>
      </div>
      {{end}}
    </div>

    <div style="text-align:center;margin-top:20px;display:flex;gap:12px;justify-content:center;">
      <a class="btn btn-secondary" href="/explore">← Back to Explore</a>
      <a class="btn btn-primary" href="/">Create New Petition</a>
    </div>
  </div>
</body>
</html>`))
