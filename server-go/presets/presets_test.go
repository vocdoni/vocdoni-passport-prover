package presets

import (
	"encoding/json"
	"testing"
)

func TestPresetsJSONValid(t *testing.T) {
	var config PresetsConfig
	if err := json.Unmarshal(presetsJSON, &config); err != nil {
		t.Fatalf("failed to parse presets JSON: %v", err)
	}

	if config.Version == "" {
		t.Error("version is empty")
	}
}

func TestGetConfig(t *testing.T) {
	config := GetConfig()
	if config == nil {
		t.Fatal("GetConfig returned nil")
	}

	if config.Version == "" {
		t.Error("config version is empty")
	}
}

func TestGenericPreset(t *testing.T) {
	preset := GetPreset("generic")
	if preset == nil {
		t.Fatal("generic preset not found")
	}

	if preset.ID != "generic" {
		t.Errorf("expected ID 'generic', got '%s'", preset.ID)
	}

	if preset.Name == "" {
		t.Error("generic preset has no name")
	}

	if preset.Fields == nil {
		t.Fatal("generic preset has no fields")
	}

	if preset.Fields.MRZ == nil {
		t.Fatal("generic preset has no MRZ fields")
	}

	// Verify standard MRZ fields exist
	requiredMRZFields := []string{
		"nationality",
		"firstname",
		"lastname",
		"birthdate",
		"expiry_date",
		"document_number",
		"gender",
	}

	for _, field := range requiredMRZFields {
		cfg, ok := preset.Fields.MRZ[field]
		if !ok {
			t.Errorf("required MRZ field '%s' not found", field)
			continue
		}
		if !cfg.Available {
			t.Errorf("MRZ field '%s' should be available", field)
		}
	}
}

func TestSpanishDNIPreset(t *testing.T) {
	preset := GetPreset("esp_dni")
	if preset == nil {
		t.Skip("Spanish DNI preset not found")
	}

	if preset.ID != "esp_dni" {
		t.Errorf("expected ID 'esp_dni', got '%s'", preset.ID)
	}

	// Check countries include ESP
	hasESP := false
	for _, c := range preset.Countries {
		if c == "ESP" {
			hasESP = true
			break
		}
	}
	if !hasESP {
		t.Error("Spanish preset should include ESP country")
	}

	// Check MRZ fields exist
	if preset.Fields.MRZ == nil {
		t.Fatal("Spanish preset should have MRZ fields")
	}

	// Spanish DNI should have optional_data_1 (DNI number)
	if cfg, ok := preset.Fields.MRZ["dni_number"]; !ok || !cfg.Available {
		t.Error("Spanish preset should have dni_number field available")
	}
}

func TestListPresets(t *testing.T) {
	presets := ListPresets()
	if len(presets) == 0 {
		t.Fatal("no presets returned")
	}

	// Generic should be first
	if presets[0].ID != "generic" {
		t.Errorf("expected first preset to be 'generic', got '%s'", presets[0].ID)
	}
}

func TestGetPresetByCountry(t *testing.T) {
	tests := []struct {
		country    string
		wantPreset string
	}{
		{"ESP", "esp_dni"},
		{"XXX", "generic"}, // Unknown country should return generic
		{"USA", "generic"}, // USA not specifically configured, should return generic
	}

	for _, tt := range tests {
		t.Run(tt.country, func(t *testing.T) {
			preset := GetPresetByCountry(tt.country)
			if preset == nil {
				t.Fatal("preset is nil")
			}
			if preset.ID != tt.wantPreset {
				t.Errorf("for country %s, expected preset '%s', got '%s'", tt.country, tt.wantPreset, preset.ID)
			}
		})
	}
}

func TestGetDisclosableFields(t *testing.T) {
	fields := GetDisclosableFields("generic")
	if fields == nil {
		t.Fatal("no fields returned for generic preset")
	}

	if len(fields) == 0 {
		t.Error("expected at least one disclosable field")
	}

	// Check that all returned fields have required properties
	for _, f := range fields {
		if f.ID == "" {
			t.Error("field has empty ID")
		}
		if f.Label == "" {
			t.Errorf("field '%s' has empty label", f.ID)
		}
		if f.Category != "mrz" {
			t.Errorf("field '%s' has invalid category: %s", f.ID, f.Category)
		}
	}
}

func TestGetFixedConstraints(t *testing.T) {
	// Spanish preset should have fixed nationality constraint
	fixed := GetFixedConstraints("esp_dni")
	if fixed == nil {
		t.Skip("no fixed constraints for Spanish preset")
	}

	// Check if nationality is fixed to ESP
	if natConstraint, ok := fixed["nationality"]; ok {
		if eq, ok := natConstraint["eq"].(string); ok {
			if eq != "ESP" {
				t.Errorf("expected fixed nationality 'ESP', got '%s'", eq)
			}
		}
	}
}

func TestListPresetSummaries(t *testing.T) {
	summaries := ListPresetSummaries()
	if len(summaries) == 0 {
		t.Fatal("no summaries returned")
	}

	for _, s := range summaries {
		if s.ID == "" {
			t.Error("summary has empty ID")
		}
		if s.Name == "" {
			t.Errorf("summary '%s' has empty name", s.ID)
		}
	}
}

func TestFieldConfigStructure(t *testing.T) {
	config := GetConfig()
	if config == nil {
		t.Fatal("config is nil")
	}

	for presetID, preset := range config.Presets {
		if preset.Fields == nil {
			continue
		}

		// Check MRZ fields
		for fieldID, cfg := range preset.Fields.MRZ {
			if cfg == nil {
				t.Errorf("preset %s: MRZ field %s has nil config", presetID, fieldID)
				continue
			}
			// Label should not be empty for available fields
			if cfg.Available && cfg.Label == "" {
				t.Errorf("preset %s: available MRZ field %s has empty label", presetID, fieldID)
			}
		}
	}
}

func TestOptionalData1Field(t *testing.T) {
	preset := GetPreset("generic")
	if preset == nil {
		t.Fatal("generic preset not found")
	}

	// optional_data_1 should be in MRZ fields
	cfg, ok := preset.Fields.MRZ["optional_data_1"]
	if !ok {
		t.Error("optional_data_1 not found in generic preset MRZ fields")
		return
	}

	if !cfg.Available {
		t.Error("optional_data_1 should be available")
	}
}
