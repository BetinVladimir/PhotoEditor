package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/styleplan"
)

func EffectiveApplyV4Style(styleRaw, overrideRaw json.RawMessage) (json.RawMessage, error) {
	if len(overrideRaw) == 0 {
		return canonical.Bytes(styleRaw)
	}
	var style map[string]any
	var envelope struct {
		Modules map[string]json.RawMessage `json:"modules"`
	}
	if json.Unmarshal(styleRaw, &style) != nil || style == nil || json.Unmarshal(overrideRaw, &envelope) != nil {
		return nil, errors.New("invalid effective apply style")
	}
	for name, raw := range envelope.Modules {
		if name == "strength" || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return nil, errors.New("invalid override module")
		}
		if name == "extensions" {
			ext, ok := style["extensions"].(map[string]any)
			if !ok {
				ext = map[string]any{}
			}
			values, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("invalid override extensions")
			}
			for key, v := range values {
				ext[key] = v
			}
			style["extensions"] = ext
		} else if name == "beauty" || name == "defects" || name == "geometry" || name == "local" || name == "culling" {
			style[name] = value
		}
	}
	return canonical.Marshal(style)
}

// ApplyV4Stage is the shared immutable stage contract. The wrapper preserves
// scheduler/API callers while Editor uses the same ordered compiler.
type ApplyV4Stage = styleplan.Stage
func CompileApplyV4Plan(raw json.RawMessage,itemCount int)([]ApplyV4Stage,error){return styleplan.Compile(raw,itemCount)}
