package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/vocdoni/vocdoni-passport-prover/server-go/presets"
	"github.com/vocdoni/vocdoni-passport-prover/server-go/storage"
)

func TestHealthEndpoint(t *testing.T) {
	logger := zerolog.Nop()
	server := NewServer(":0", nil, nil, "", logger)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp["status"])
	}
}

func TestBuildPetitionPayload(t *testing.T) {
	tests := []struct {
		name             string
		petition         *storage.Petition
		aggregateBaseURL string
		petitionBaseURL  string
		wantAggregateURL string
		wantInfoURL      string
	}{
		{
			name: "basic petition",
			petition: &storage.Petition{
				PetitionID: "test-123",
				Name:       "Test Petition",
				Purpose:    "Testing",
				Scope:      "test",
				Query: map[string]any{
					"nationality": map[string]any{"disclose": true},
				},
			},
			aggregateBaseURL: "https://example.com",
			petitionBaseURL:  "https://example.com",
			wantAggregateURL: "https://example.com/api/proofs/aggregate",
			wantInfoURL:      "https://example.com/petition/test-123",
		},
		{
			name: "petition with trailing slash",
			petition: &storage.Petition{
				PetitionID: "test-456",
				Name:       "Another Test",
				Purpose:    "More Testing",
				Scope:      "test",
				Query:      map[string]any{},
			},
			aggregateBaseURL: "https://example.com/",
			petitionBaseURL:  "https://example.com/",
			wantAggregateURL: "https://example.com/api/proofs/aggregate",
			wantInfoURL:      "https://example.com/petition/test-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := buildPetitionPayload(tt.petition, tt.aggregateBaseURL, tt.petitionBaseURL)

			if payload["kind"] != "vocdoni-passport-request" {
				t.Errorf("expected kind 'vocdoni-passport-request', got '%v'", payload["kind"])
			}

			if payload["version"] != 1 {
				t.Errorf("expected version 1, got %v", payload["version"])
			}

			aggregateURL, ok := payload["aggregateUrl"].(string)
			if !ok {
				t.Fatal("aggregateUrl is not a string")
			}

			if aggregateURL != tt.wantAggregateURL {
				t.Errorf("expected aggregateUrl '%s', got '%s'", tt.wantAggregateURL, aggregateURL)
			}

			infoURL, ok := payload["infoUrl"].(string)
			if !ok {
				t.Fatal("infoUrl is not a string")
			}

			if infoURL != tt.wantInfoURL {
				t.Errorf("expected infoUrl '%s', got '%s'", tt.wantInfoURL, infoURL)
			}

			if payload["petitionId"] != tt.petition.PetitionID {
				t.Errorf("expected petitionId '%s', got '%v'", tt.petition.PetitionID, payload["petitionId"])
			}

			service, ok := payload["service"].(map[string]any)
			if !ok {
				t.Fatal("service is not a map")
			}

			if service["name"] != tt.petition.Name {
				t.Errorf("expected service name '%s', got '%v'", tt.petition.Name, service["name"])
			}
		})
	}
}

func TestAppLinkBaseURL(t *testing.T) {
	t.Setenv("VOCDONI_PUBLIC_BASE_URL", "https://api.example.com")
	t.Setenv("VOCDONI_DEEPLINK_BASE_URL", "https://vocdoni.link")

	req := httptest.NewRequest(http.MethodGet, "/petition/test-123", nil)
	req.Host = "ignored.example.com"

	if got := appLinkBaseURL(req); got != "https://vocdoni.link" {
		t.Fatalf("expected deeplink base https://vocdoni.link, got %s", got)
	}
}

func TestPetitionDeepLinkURL(t *testing.T) {
	t.Setenv("VOCDONI_PUBLIC_BASE_URL", "https://passport-sign.vocdoni.net")
	t.Setenv("VOCDONI_DEEPLINK_BASE_URL", "https://vocdoni.link")

	req := httptest.NewRequest(http.MethodGet, "/petition/test-123", nil)
	req.Host = "ignored.example.com"

	got := petitionDeepLinkURL(req, "test-123")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("failed to parse deep link: %v", err)
	}

	if parsed.Scheme != "https" || parsed.Host != "vocdoni.link" || parsed.Path != "/passport" {
		t.Fatalf("unexpected deep link URL %s", got)
	}

	sign := parsed.Query().Get("sign")
	if sign == "" {
		t.Fatalf("expected sign query parameter in %s", got)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(sign)
	if err != nil {
		t.Fatalf("failed to decode sign payload: %v", err)
	}

	if string(decoded) != "passport-sign.vocdoni.net|test-123" {
		t.Fatalf("unexpected sign payload %q", string(decoded))
	}
}

func TestPetitionDeepLinkURLFromValues(t *testing.T) {
	got := petitionDeepLinkURLFromValues("https://passport-sign.vocdoni.net", "https://vocdoni.link", "test-456")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("failed to parse deep link: %v", err)
	}

	if parsed.Scheme != "https" || parsed.Host != "vocdoni.link" || parsed.Path != "/passport" {
		t.Fatalf("unexpected deep link URL %s", got)
	}

	sign := parsed.Query().Get("sign")
	if sign == "" {
		t.Fatalf("expected sign query parameter in %s", got)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(sign)
	if err != nil {
		t.Fatalf("failed to decode sign payload: %v", err)
	}

	if string(decoded) != "passport-sign.vocdoni.net|test-456" {
		t.Fatalf("unexpected sign payload %q", string(decoded))
	}
}

func TestRequestDeepLinkURL(t *testing.T) {
	t.Setenv("VOCDONI_DEEPLINK_BASE_URL", "https://vocdoni.link")

	req := httptest.NewRequest(http.MethodGet, "/api/request-qr.png", nil)
	payload := []byte(`{"kind":"vocdoni-passport-request","aggregateUrl":"https://passport-sign.vocdoni.net/api/proofs/aggregate"}`)

	got := requestDeepLinkURL(req, payload)
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("failed to parse deep link: %v", err)
	}

	if parsed.Scheme != "https" || parsed.Host != "vocdoni.link" || parsed.Path != "/passport" {
		t.Fatalf("unexpected request deep link URL %s", got)
	}

	requestParam := parsed.Query().Get("request")
	if requestParam == "" {
		t.Fatalf("expected request query parameter in %s", got)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(requestParam)
	if err != nil {
		t.Fatalf("failed to decode request payload: %v", err)
	}

	if string(decoded) != string(payload) {
		t.Fatalf("unexpected request payload %q", string(decoded))
	}
}

func TestPetitionPageTemplateUsesDeepLinkCopy(t *testing.T) {
	var out bytes.Buffer
	data := petitionPageData{
		Petition: &storage.Petition{
			PetitionID:      "petition-123",
			Name:            "Test Petition",
			Purpose:         "Testing",
			Scope:           "test-scope",
			DisclosedFields: []string{"nationality"},
			SignatureCount:  3,
		},
		TotalSignatures: 3,
		BaseURL:         "https://passport-sign.vocdoni.net",
		DeepLinkURL:     "https://vocdoni.link/passport?sign=abc123",
	}

	if err := petitionPageTemplate.Execute(&out, data); err != nil {
		t.Fatalf("failed to render petition page template: %v", err)
	}

	html := out.String()
	if !strings.Contains(html, `id="deepLinkUrl" value="https://vocdoni.link/passport?sign=abc123"`) {
		t.Fatalf("expected petition page to include deeplink URL, got %s", html)
	}
	if strings.Contains(html, `window.location.href`) {
		t.Fatalf("petition page should not copy the browser URL anymore")
	}
	if !strings.Contains(html, `onclick="copyLink(this)"`) {
		t.Fatalf("petition page copy button should pass the button element")
	}
	if strings.Contains(html, `printf "%q"`) {
		t.Fatalf("petition page should not embed the deeplink as a quoted JS literal")
	}
}

func TestPresetsLoading(t *testing.T) {
	config := presets.GetConfig()
	if config == nil {
		t.Fatal("presets config is nil")
	}

	if config.Version == "" {
		t.Error("presets version is empty")
	}

	if len(config.Presets) == 0 {
		t.Error("no presets loaded")
	}

	// Check generic preset exists
	generic := presets.GetPreset("generic")
	if generic == nil {
		t.Fatal("generic preset not found")
	}

	if generic.Name == "" {
		t.Error("generic preset has no name")
	}

	if generic.Fields == nil {
		t.Error("generic preset has no fields")
	}

	// Check MRZ fields exist
	if generic.Fields.MRZ == nil {
		t.Error("generic preset has no MRZ fields")
	}

	// Check that nationality field exists in MRZ
	if _, ok := generic.Fields.MRZ["nationality"]; !ok {
		t.Error("nationality field not found in generic preset MRZ fields")
	}
}

func TestSpanishPreset(t *testing.T) {
	esp := presets.GetPreset("esp_dni")
	if esp == nil {
		t.Skip("Spanish DNI preset not found")
	}

	if esp.Fields == nil {
		t.Fatal("Spanish preset has no fields")
	}

	// Check MRZ fields exist
	if esp.Fields.MRZ == nil {
		t.Fatal("Spanish preset has no MRZ fields")
	}

	// Spanish DNI should have dni_number field
	if cfg, ok := esp.Fields.MRZ["dni_number"]; !ok || !cfg.Available {
		t.Error("dni_number field should be available in Spanish preset")
	}
}

func TestDisclosableFields(t *testing.T) {
	fields := presets.GetDisclosableFields("generic")
	if fields == nil {
		t.Fatal("no disclosable fields for generic preset")
	}

	// Should have at least nationality
	hasNationality := false
	for _, f := range fields {
		if f.ID == "nationality" {
			hasNationality = true
			break
		}
	}

	if !hasNationality {
		t.Error("nationality field not found in disclosable fields")
	}
}

func TestQueryValidation(t *testing.T) {
	tests := []struct {
		name    string
		query   map[string]any
		wantErr bool
	}{
		{
			name: "valid nationality disclose",
			query: map[string]any{
				"nationality": map[string]any{"disclose": true},
			},
			wantErr: false,
		},
		{
			name: "valid age constraint",
			query: map[string]any{
				"age": map[string]any{"gte": 18},
			},
			wantErr: false,
		},
		{
			name: "valid optional_data_1",
			query: map[string]any{
				"optional_data_1": map[string]any{"disclose": true},
			},
			wantErr: false,
		},
		{
			name: "combined fields",
			query: map[string]any{
				"nationality":     map[string]any{"disclose": true},
				"optional_data_1": map[string]any{"disclose": true},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the query can be marshaled/unmarshaled
			data, err := json.Marshal(tt.query)
			if err != nil {
				t.Fatalf("failed to marshal query: %v", err)
			}

			var parsed map[string]any
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("failed to unmarshal query: %v", err)
			}
		})
	}
}

func TestCreatePetitionRequest(t *testing.T) {
	// Test the petition creation request format
	reqBody := map[string]any{
		"name":    "Test Petition",
		"purpose": "Testing the API",
		"scope":   "test",
		"preset":  "generic",
		"query": map[string]any{
			"nationality": map[string]any{"disclose": true},
			"age":         map[string]any{"gte": 18},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	// Verify it can be parsed back
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if parsed["name"] != "Test Petition" {
		t.Errorf("expected name 'Test Petition', got '%v'", parsed["name"])
	}

	query, ok := parsed["query"].(map[string]any)
	if !ok {
		t.Fatal("query is not a map")
	}

	nat, ok := query["nationality"].(map[string]any)
	if !ok {
		t.Fatal("nationality is not a map")
	}

	if nat["disclose"] != true {
		t.Error("nationality disclose should be true")
	}
}
