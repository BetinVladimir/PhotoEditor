package styleplan

import (
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/canonical"
)

// AuthoredBasePlan is a compiler output, not a SQL admission or a completed
// receipt. The immutable parent must retain these exact bytes before dispatch.
// OriginalRecipe is deliberately outside the effective-style object: layers,
// session/learning/override witnesses remain in the unchanged parent closure.
type AuthoredBasePlan struct {
	EffectiveStyle json.RawMessage `json:"effective_style"`
	Stages         []Stage         `json:"stages"`
	SHA256         string          `json:"sha256"`
}

// CompileAuthoredBasePlan uses the production Apply stage compiler without an
// Editor precompose prefix. Prefixing editor.layers_precompose would execute the
// authored stack before its base, and subsequently executing the continuation
// would apply those layers twice. No module is removed from the effective style.
func CompileAuthoredBasePlan(effectiveStyle json.RawMessage, itemCount int) (AuthoredBasePlan, error) {
	raw, err := canonical.Bytes(effectiveStyle)
	if err != nil {
		return AuthoredBasePlan{}, err
	}
	stages, err := Compile(raw, itemCount)
	if err != nil {
		return AuthoredBasePlan{}, err
	}
	if len(stages) == 0 {
		return AuthoredBasePlan{}, errors.New("authored base plan empty")
	}
	for _, stage := range stages {
		if stage.Phase == "editor_precompose" || stage.Operation == "editor.layers_precompose" {
			return AuthoredBasePlan{}, errors.New("authored base cannot execute authored layers")
		}
	}
	body := struct {
		Style  json.RawMessage `json:"effective_style"`
		Stages []Stage         `json:"stages"`
	}{raw, stages}
	digest, err := canonical.Hash(body)
	if err != nil {
		return AuthoredBasePlan{}, err
	}
	return AuthoredBasePlan{EffectiveStyle: raw, Stages: stages, SHA256: digest}, nil
}
