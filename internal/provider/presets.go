package provider

import (
	_ "embed"
	"encoding/json"

	"github.com/shaoxier/AIofCage/internal/session"
)

// Preset pre-fills the add-provider form. Model names change often; users can edit every field.
type Preset struct {
	ID             string        `json:"id"`
	Agent          session.Agent `json:"agent"`
	Name           string        `json:"name"`
	Protocol       Protocol      `json:"protocol"`
	BaseURL        string        `json:"base_url"`
	Model          string        `json:"model"`
	Auth           Auth          `json:"auth"`
	ExistingKeyRef string        `json:"existing_key_ref,omitempty"`
	Note           string        `json:"note,omitempty"`
}

//go:embed presets.json
var presetsJSON []byte

func Presets() []Preset {
	var presets []Preset
	if err := json.Unmarshal(presetsJSON, &presets); err != nil {
		panic("presets.json 格式错误: " + err.Error())
	}
	return presets
}
