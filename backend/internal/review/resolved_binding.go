package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"photoeditor/backend/internal/canonical"
)

// ResolvedBinding is a provenance projection, not numerical style validation.
// Callers must independently attest the canonical style.resolve producer.
// The worker validates the complete ResolvedStyle before executing any pixels.
type ResolvedBinding struct {
	Image ArtifactRef
	Style ArtifactRef
	Mode  string
	Hash  string
}

type resolveBindingParams struct {
	Style            ArtifactRef  `json:"style"`
	Image            ArtifactRef  `json:"image"`
	Stats            *ArtifactRef `json:"stats"`
	StrictOOD        bool         `json:"strict_ood"`
	GroupPrototypeID *string      `json:"group_prototype_id"`
}

// ParseResolvedBinding verifies the hash over ALL body fields, including fields
// this backend projection deliberately leaves to the processing worker.
func ParseResolvedBinding(raw []byte) (ResolvedBinding, error) {
	fail := func() (ResolvedBinding, error) {
		return ResolvedBinding{}, errors.New("invalid resolved style binding")
	}
	if len(raw) > 1048576 {
		return fail()
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return fail()
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(normalized, &body) != nil {
		return fail()
	}
	keys := []string{"params", "mode", "fixed", "absolute_lut", "local", "strength", "modules", "neighbors", "distance", "uncovered_fraction", "reasons", "needs_review", "hash"}
	if len(body) != len(keys) {
		return fail()
	}
	for _, key := range keys {
		if _, ok := body[key]; !ok {
			return fail()
		}
	}
	var params resolveBindingParams
	if decodeExact(body["params"], &params) != nil || !params.Image.valid() || !params.Style.valid() || (params.Stats != nil && !params.Stats.valid()) {
		return fail()
	}
	var mode, hash string
	if json.Unmarshal(body["mode"], &mode) != nil || json.Unmarshal(body["hash"], &hash) != nil {
		return fail()
	}
	switch mode {
	case "fixed", "adaptive", "fixed_fallback", "group_fixed", "skip":
	default:
		return fail()
	}
	if (mode == "group_fixed") != (params.GroupPrototypeID != nil) || (mode == "skip" && !params.StrictOOD) {
		return fail()
	}
	if params.GroupPrototypeID != nil && (utf8.RuneCountInString(*params.GroupPrototypeID) < 1 || utf8.RuneCountInString(*params.GroupPrototypeID) > 120) {
		return fail()
	}
	delete(body, "hash")
	payload, err := canonical.Marshal(body)
	if err != nil {
		return fail()
	}
	digest := sha256.Sum256(payload)
	if hash != hex.EncodeToString(digest[:]) {
		return fail()
	}
	return ResolvedBinding{Image: params.Image, Style: params.Style, Mode: mode, Hash: hash}, nil
}
