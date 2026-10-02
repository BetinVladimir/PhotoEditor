package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// LensQualificationPin mirrors the worker's operator-provisioned registry.
// Pins and runtime SHA must come from trusted deployment configuration, never
// from an API request, editor recipe or the profile document being admitted.
type LensQualificationPin struct {
	Qualification                               Ref
	CoefficientsSHA, RuntimeLockSHA, DatasetSHA string
}

// VerifyQualifiedLensProfile binds exact immutable profile bytes, coefficients,
// qualification evidence and the installed runtime. It does not create evidence
// or infer qualification from a profile-supplied report reference.
func VerifyQualifiedLensProfile(raw []byte, expected Ref, runtimeSHA string, pins []LensQualificationPin) (LensProfileDocument, error) {
	if !validRef(expected) || !digestPattern.MatchString(runtimeSHA) || len(pins) < 1 || len(pins) > 10000 {
		return LensProfileDocument{}, ErrInvalid
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expected.SHA256 {
		return LensProfileDocument{}, ErrInvalid
	}
	profile, err := ParseLensProfile(raw)
	if err != nil {
		return LensProfileDocument{}, err
	}
	var coefficients map[string]json.RawMessage
	if json.Unmarshal(raw, &coefficients) != nil {
		return LensProfileDocument{}, ErrInvalid
	}
	delete(coefficients, "qualification")
	coefficientsSHA, err := canonical.Hash(coefficients)
	if err != nil {
		return LensProfileDocument{}, err
	}
	matches := 0
	for _, pin := range pins {
		if !validRef(pin.Qualification) || !digestPattern.MatchString(pin.CoefficientsSHA) || !digestPattern.MatchString(pin.RuntimeLockSHA) || !digestPattern.MatchString(pin.DatasetSHA) {
			return LensProfileDocument{}, ErrInvalid
		}
		if pin.Qualification == profile.Qualification && pin.CoefficientsSHA == coefficientsSHA && pin.RuntimeLockSHA == runtimeSHA {
			matches++
		}
	}
	if matches != 1 {
		return LensProfileDocument{}, ErrInvalid
	}
	return profile, nil
}
