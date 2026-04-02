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
	Title             string
	PayloadJSON       string
	PayloadB64        string
	AggregateURL      string
	APKDownloadURL    string
	RequestConfigURL  string
	NationalityIn     string
	NationalityOut    string
	IssuingCountryIn  string
	IssuingCountryOut string
	Disclose          string
	AgeGte            string
	Purpose           string
	Scope             string
	PetitionID        string
	Name              string
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
	payload, err := buildPayloadFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	svg, err := qrcode.Encode(string(body), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(svg)
}

func (s *Server) handleRequestPage(w http.ResponseWriter, r *http.Request) {
	payload, err := buildPayloadFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pretty, _ := json.MarshalIndent(payload, "", "  ")
	data := requestPageData{
		Title:            "Vocdoni Passport Request",
		PayloadJSON:      string(pretty),
		PayloadB64:       base64.RawURLEncoding.EncodeToString(pretty),
		AggregateURL:     payload.AggregateURL,
		APKDownloadURL:   baseURL(r) + "/downloads/app-release.apk",
		RequestConfigURL: baseURL(r) + "/api/request-config",
		Purpose:          payload.Service.Purpose,
		Scope:            payload.Service.Scope,
		PetitionID:       payload.PetitionID,
		Name:             payload.Service.Name,
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
		aggregateURL = baseURL(r) + "/api/proofs/aggregate"
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

func publicHost(r *http.Request) string {
	if configured := configuredPublicBaseURL(); configured != "" {
		parsed, err := url.Parse(configured)
		if err == nil && parsed.Host != "" {
			return hostOnly(parsed.Host)
		}
	}
	return hostOnly(r.Host)
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
    body { font-family: system-ui, sans-serif; background:#f3f4f6; color:#111827; margin:0; }
    .wrap { max-width: 980px; margin: 0 auto; padding: 24px; }
    .grid { display:grid; grid-template-columns: 1.2fr 1fr; gap: 20px; }
    .card { background:#fff; border-radius:12px; padding:18px; box-shadow:0 1px 3px rgba(0,0,0,.08); margin-bottom:16px; }
    h1,h2 { margin:0 0 12px 0; }
    label { display:block; font-weight:600; margin:10px 0 6px 0; }
    input, textarea { width:100%; box-sizing:border-box; padding:10px; border-radius:8px; border:1px solid #d1d5db; font-size:14px; }
    textarea { min-height: 140px; font-family: ui-monospace, monospace; }
    .qr { text-align:center; }
    .qr img { width: 320px; height: 320px; max-width:100%; border-radius:12px; background:#fff; }
    .muted { color:#6b7280; font-size:14px; }
    .chips span { display:inline-block; margin:4px 6px 0 0; background:#eef2ff; color:#3730a3; border-radius:999px; padding:6px 10px; font-size:12px; }
    .button { display:inline-flex; align-items:center; justify-content:center; padding:10px 14px; border-radius:10px; background:#111827; color:#fff; text-decoration:none; font-weight:600; }
    .button.secondary { background:#eef2ff; color:#1d4ed8; }
    .stack { display:flex; flex-direction:column; gap:10px; }
    .row { display:flex; gap:10px; flex-wrap:wrap; }
    @media (max-width: 800px) { .grid { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="card">
      <h1>🛂 Vocdoni Passport Request</h1>
      <div class="muted">This QR is scanned by the mobile app. It contains the aggregation URL plus zkPassport-style request metadata.</div>
    </div>
    <div class="grid">
      <div>
        <div class="card">
          <h2>Request configuration</h2>
          <label>Name</label><input id="name" value="{{.Name}}" />
          <label>Purpose</label><input id="purpose" value="{{.Purpose}}" />
          <label>Scope</label><input id="scope" value="{{.Scope}}" />
          <label>Petition ID</label><input id="petitionId" value="{{.PetitionID}}" />
          <label>Aggregation URL</label><input id="aggregateUrl" value="{{.AggregateURL}}" />
          <label>Nationality include (CSV alpha-3)</label><input id="nationalityIn" value="{{.NationalityIn}}" placeholder="ESP,FRA,DEU" />
          <label>Nationality exclude (CSV alpha-3)</label><input id="nationalityOut" value="{{.NationalityOut}}" placeholder="RUS,BLR" />
          <label>Issuing country include (CSV alpha-3)</label><input id="issuingCountryIn" value="{{.IssuingCountryIn}}" placeholder="ESP,FRA" />
          <label>Issuing country exclude (CSV alpha-3)</label><input id="issuingCountryOut" value="{{.IssuingCountryOut}}" placeholder="RUS,BLR" />
          <label>Disclose fields (CSV)</label><input id="disclose" value="{{.Disclose}}" placeholder="firstname,nationality,document_type" />
          <label>Minimum age</label><input id="ageGte" value="{{.AgeGte}}" placeholder="18" />
        </div>
        <div class="card">
          <h2>Payload JSON</h2>
          <textarea id="payload" readonly>{{.PayloadJSON}}</textarea>
        </div>
      </div>
      <div>
        <div class="card qr">
          <h2>Scan in the app</h2>
          <img id="qr" src="/api/request-qr.png?payload={{.PayloadB64}}" alt="QR code" />
          <p class="muted">Open the mobile app → Scan Server QR</p>
          <div class="stack">
            <a class="button" href="{{.APKDownloadURL}}">Download Android APK</a>
            <label>Copyable request link</label>
            <input id="requestLink" value="{{.RequestConfigURL}}" readonly />
            <div class="row">
              <button class="button secondary" type="button" onclick="copyRequestLink()">Copy request link</button>
              <a class="button secondary" id="requestLinkOpen" href="{{.RequestConfigURL}}" target="_blank" rel="noreferrer">Open JSON</a>
            </div>
          </div>
        </div>
        <div class="card">
          <h2>Requested constraints</h2>
          <div class="chips" id="chips"></div>
        </div>
      </div>
    </div>
  </div>
<script>
function splitCsv(v){return v.split(',').map(x=>x.trim()).filter(Boolean)}
function buildRequestLink(){
  const params = new URLSearchParams();
  const fields = {
    name: document.getElementById('name').value.trim(),
    purpose: document.getElementById('purpose').value.trim(),
    scope: document.getElementById('scope').value.trim(),
    petitionId: document.getElementById('petitionId').value.trim(),
    aggregateUrl: document.getElementById('aggregateUrl').value.trim(),
    nationalityIn: document.getElementById('nationalityIn').value.trim(),
    nationalityOut: document.getElementById('nationalityOut').value.trim(),
    issuingCountryIn: document.getElementById('issuingCountryIn').value.trim(),
    issuingCountryOut: document.getElementById('issuingCountryOut').value.trim(),
    disclose: document.getElementById('disclose').value.trim(),
    ageGte: document.getElementById('ageGte').value.trim(),
  };
  for (const [key, value] of Object.entries(fields)) {
    if (value) params.set(key, value);
  }
  return '{{.RequestConfigURL}}' + (params.toString() ? '?' + params.toString() : '');
}
function buildPayload(){
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
  const inVals = splitCsv(document.getElementById('nationalityIn').value.toUpperCase());
  const outVals = splitCsv(document.getElementById('nationalityOut').value.toUpperCase());
  const issuingInVals = splitCsv(document.getElementById('issuingCountryIn').value.toUpperCase());
  const issuingOutVals = splitCsv(document.getElementById('issuingCountryOut').value.toUpperCase());
  const discloseVals = splitCsv(document.getElementById('disclose').value);
  const age = parseInt(document.getElementById('ageGte').value.trim(), 10);
  if (inVals.length || outVals.length) {
    query.nationality = {};
    if (inVals.length) query.nationality.in = inVals;
    if (outVals.length) query.nationality.out = outVals;
  }
  if (issuingInVals.length || issuingOutVals.length) {
    query.issuing_country = {};
    if (issuingInVals.length) query.issuing_country.in = issuingInVals;
    if (issuingOutVals.length) query.issuing_country.out = issuingOutVals;
  }
  if (!Number.isNaN(age) && age > 0) query.age = { gte: age };
  for (const f of discloseVals) query[f] = { disclose: true };
  if (Object.keys(query).length) payload.query = query;
  return payload;
}
function render(){
  const payload = buildPayload();
  const json = JSON.stringify(payload, null, 2);
  document.getElementById('payload').value = json;
  const b64 = btoa(unescape(encodeURIComponent(json))).replace(/\+/g,'-').replace(/\//g,'_').replace(/=+$/,'');
  document.getElementById('qr').src = '/api/request-qr.png?payload=' + encodeURIComponent(b64);
  const requestLink = buildRequestLink();
  document.getElementById('requestLink').value = requestLink;
  document.getElementById('requestLinkOpen').href = requestLink;
  const chips = [];
  if (payload.query?.nationality?.in) chips.push('nationality in: ' + payload.query.nationality.in.join(', '));
  if (payload.query?.nationality?.out) chips.push('nationality out: ' + payload.query.nationality.out.join(', '));
  if (payload.query?.issuing_country?.in) chips.push('issuing country in: ' + payload.query.issuing_country.in.join(', '));
  if (payload.query?.issuing_country?.out) chips.push('issuing country out: ' + payload.query.issuing_country.out.join(', '));
  if (payload.query?.age?.gte) chips.push('age ≥ ' + payload.query.age.gte);
  for (const [k,v] of Object.entries(payload.query || {})) if (v && v.disclose) chips.push('disclose: ' + k);
  document.getElementById('chips').innerHTML = chips.map(c => '<span>' + c + '</span>').join('') || '<span>No extra constraints</span>';
}
async function copyRequestLink(){
  const value = document.getElementById('requestLink').value;
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    document.getElementById('requestLink').select();
    document.execCommand('copy');
  }
}
for (const id of ['name','purpose','scope','petitionId','aggregateUrl','nationalityIn','nationalityOut','issuingCountryIn','issuingCountryOut','disclose','ageGte']) {
  document.getElementById(id).addEventListener('input', render);
}
render();
</script>
</body>
</html>`))
