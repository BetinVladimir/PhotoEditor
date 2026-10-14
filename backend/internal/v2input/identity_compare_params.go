package v2input

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"photoeditor/backend/internal/canonical"
)

// IdentityCompareParams is the exact V23 worker input contract. The caller
// must independently establish source lineage, consent, calibration and model
// qualification before publishing or admitting the task.
type IdentityCompareParams struct {
	SchemaVersion int                   `json:"schema_version"`
	Before        SessionVersionBinding `json:"before"`
	After         SessionVersionBinding `json:"after"`
	EffectiveMask Ref                   `json:"effective_mask"`
	Protected     Ref                   `json:"protected"`
	ModelLockSHA  string                `json:"model_lock_sha"`
	Calibration   Ref                   `json:"calibration"`
	Consents      Ref                   `json:"consents"`
}

func validIdentityCompareParams(p IdentityCompareParams) bool {
	if p.SchemaVersion != 1 || !semanticBindingValid(p.Before) || !semanticBindingValid(p.After) ||
		p.Before.AssetVersionID == p.After.AssetVersionID || p.Before.Source == p.After.Source ||
		!validRef(p.EffectiveMask) || !validRef(p.Protected) ||
		!validRef(p.Calibration) || !validRef(p.Consents) ||
		!digestPattern.MatchString(p.ModelLockSHA) {
		return false
	}
	seen := map[string]bool{}
	for _, ref := range []Ref{p.Before.Source, p.After.Source, p.EffectiveMask, p.Protected, p.Calibration, p.Consents} {
		id := ref.ArtifactID.String()
		if seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func CompileIdentityCompareParams(p IdentityCompareParams) ([]byte, string, error) {
	if !validIdentityCompareParams(p) {
		return nil, "", ErrInvalid
	}
	body, err := canonical.Marshal(p)
	if err != nil || len(body) < 2 || len(body) > 1<<20 {
		return nil, "", ErrInvalid
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), nil
}

// DecodeIdentityCompareParams rejects undeclared keys, nulls and alternate
// encodings. A canonical body is required because its digest pins task Params.
func DecodeIdentityCompareParams(raw []byte) (IdentityCompareParams, error) {
	var empty IdentityCompareParams
	if len(raw) < 2 || len(raw) > 1<<20 {
		return empty, ErrInvalid
	}
	fields, ok := semanticObject(raw, nil, "schema_version", "before", "after", "effective_mask", "protected", "model_lock_sha", "calibration", "consents")
	if !ok || !semanticBindingJSON(fields["before"]) || !semanticBindingJSON(fields["after"]) ||
		!semanticRefJSON(fields["effective_mask"]) || !semanticRefJSON(fields["protected"]) ||
		!semanticRefJSON(fields["calibration"]) || !semanticRefJSON(fields["consents"]) {
		return empty, ErrInvalid
	}
	var p IdentityCompareParams
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF {
		return empty, ErrInvalid
	}
	canonicalBody, _, err := CompileIdentityCompareParams(p)
	if err != nil || !bytes.Equal(raw, canonicalBody) {
		return empty, ErrInvalid
	}
	return p, nil
}
