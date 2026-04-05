package presets

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed country_presets.json
var presetsJSON []byte

type PresetsConfig struct {
	Version         string                     `json:"version"`
	Presets         map[string]*CountryPreset  `json:"presets"`
	FieldCategories map[string][]string        `json:"fieldCategories"`
}

type CountryPreset struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Countries     []string               `json:"countries"`
	DocumentTypes []string               `json:"documentTypes"`
	Fields        *PresetFields          `json:"fields"`
	Constraints   map[string]*Constraint `json:"constraints"`
}

type PresetFields struct {
	MRZ map[string]*FieldConfig `json:"mrz"`
}

type FieldConfig struct {
	Label           string          `json:"label"`
	Description     string          `json:"description"`
	Example         string          `json:"example"`
	Available       bool            `json:"available"`
	Fixed           bool            `json:"fixed,omitempty"`
	FixedValue      string          `json:"fixedValue,omitempty"`
	MapsTo          string          `json:"mapsTo,omitempty"`
	RequiresRawData bool            `json:"requiresRawData,omitempty"`
	TD1Only         bool            `json:"td1Only,omitempty"`
	MRZPosition     *MRZPosition    `json:"mrzPosition,omitempty"`
}

type MRZPosition struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type Constraint struct {
	Configurable bool           `json:"configurable"`
	Fixed        bool           `json:"fixed,omitempty"`
	FixedValue   map[string]any `json:"fixedValue,omitempty"`
	DefaultValue map[string]any `json:"defaultValue,omitempty"`
}

var loadedConfig *PresetsConfig

func init() {
	if err := json.Unmarshal(presetsJSON, &loadedConfig); err != nil {
		panic(fmt.Sprintf("failed to parse embedded presets: %v", err))
	}
}

func GetConfig() *PresetsConfig {
	return loadedConfig
}

func GetPreset(id string) *CountryPreset {
	if loadedConfig == nil {
		return nil
	}
	return loadedConfig.Presets[id]
}

func ListPresets() []*CountryPreset {
	if loadedConfig == nil {
		return nil
	}
	presets := make([]*CountryPreset, 0, len(loadedConfig.Presets))
	for _, p := range loadedConfig.Presets {
		presets = append(presets, p)
	}
	sort.Slice(presets, func(i, j int) bool {
		if presets[i].ID == "generic" {
			return true
		}
		if presets[j].ID == "generic" {
			return false
		}
		return presets[i].Name < presets[j].Name
	})
	return presets
}

func GetPresetByCountry(countryCode string) *CountryPreset {
	if loadedConfig == nil {
		return nil
	}
	for _, preset := range loadedConfig.Presets {
		for _, country := range preset.Countries {
			if country == countryCode {
				return preset
			}
		}
	}
	return loadedConfig.Presets["generic"]
}

type PresetSummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Countries     []string `json:"countries"`
	DocumentTypes []string `json:"documentTypes"`
}

func ListPresetSummaries() []*PresetSummary {
	presets := ListPresets()
	summaries := make([]*PresetSummary, len(presets))
	for i, p := range presets {
		summaries[i] = &PresetSummary{
			ID:            p.ID,
			Name:          p.Name,
			Description:   p.Description,
			Countries:     p.Countries,
			DocumentTypes: p.DocumentTypes,
		}
	}
	return summaries
}

type DisclosableField struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Description     string `json:"description"`
	Example         string `json:"example"`
	Category        string `json:"category"`
	RequiresRawData bool   `json:"requiresRawData,omitempty"`
	Fixed           bool   `json:"fixed,omitempty"`
}

func GetDisclosableFields(presetID string) []*DisclosableField {
	preset := GetPreset(presetID)
	if preset == nil {
		preset = GetPreset("generic")
	}
	if preset == nil || preset.Fields == nil {
		return nil
	}

	var fields []*DisclosableField

	for id, cfg := range preset.Fields.MRZ {
		if id == "_note" || !cfg.Available {
			continue
		}
		fields = append(fields, &DisclosableField{
			ID:          id,
			Label:       cfg.Label,
			Description: cfg.Description,
			Example:     cfg.Example,
			Category:    "mrz",
			Fixed:       cfg.Fixed,
		})
	}

	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Label < fields[j].Label
	})

	return fields
}

func GetFixedConstraints(presetID string) map[string]map[string]any {
	preset := GetPreset(presetID)
	if preset == nil {
		return nil
	}

	fixed := make(map[string]map[string]any)
	for name, constraint := range preset.Constraints {
		if constraint.Fixed && constraint.FixedValue != nil {
			fixed[name] = constraint.FixedValue
		}
	}
	return fixed
}
