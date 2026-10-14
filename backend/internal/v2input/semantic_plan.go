package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type SemanticPlan struct {
	SchemaVersion  int                 `json:"schema_version"`
	IntentSHA      string              `json:"intent_sha"`
	SnapshotDigest string              `json:"snapshot_digest"`
	Candidates     []SemanticCandidate `json:"candidates"`
	ModelLockSHA   string              `json:"model_lock_sha"`
	Calibration    Ref                 `json:"calibration"`
}

// DecodeSemanticPlan owns the document and validates embedded candidates with
// the same strict contract used for individually authenticated producer outputs.
func DecodeSemanticPlan(raw []byte) (SemanticPlan, error) {
	var empty SemanticPlan
	if len(raw) == 0 || len(raw) > 8<<20 {
		return empty, ErrInvalid
	}
	fields, ok := semanticObject(raw, nil, "schema_version", "intent_sha", "snapshot_digest", "candidates", "model_lock_sha", "calibration")
	var plan SemanticPlan
	if !ok || !semanticRefJSON(fields["calibration"]) || !semanticDecode(raw, &plan) || plan.SchemaVersion != 1 || !validRef(plan.Calibration) || !digestPattern.MatchString(plan.IntentSHA) || !digestPattern.MatchString(plan.SnapshotDigest) || !digestPattern.MatchString(plan.ModelLockSHA) {
		return empty, ErrInvalid
	}
	var candidates []json.RawMessage
	if json.Unmarshal(fields["candidates"], &candidates) != nil || len(candidates) < 1 || len(candidates) > 5000 {
		return empty, ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	plan.Candidates = nil
	for _, rawCandidate := range candidates {
		candidate, err := DecodeSemanticCandidate(rawCandidate)
		if err != nil || seen[candidate.Target.AssetVersionID] {
			return empty, ErrInvalid
		}
		seen[candidate.Target.AssetVersionID] = true
		plan.Candidates = append(plan.Candidates, candidate)
	}
	return plan, nil
}

// VerifySemanticPlan binds exact output bytes to frozen assembly inputs. Scoped
// producer adoption and current authorization remain repository responsibilities.
func VerifySemanticPlan(ref Ref, raw []byte, params SemanticAssembleParams, snapshotRef Ref, snapshotRaw []byte, documents []SemanticCandidateDocument) (SemanticPlan, error) {
	var empty SemanticPlan
	if !validRef(ref) || len(raw) == 0 || len(raw) > 8<<20 {
		return empty, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return empty, ErrInvalid
	}
	plan, err := DecodeSemanticPlan(raw)
	if err != nil {
		return empty, err
	}
	paramsRaw, err := canonical.Marshal(params)
	if err != nil {
		return empty, ErrInvalid
	}
	owned, err := DecodeSemanticAssembleParams(paramsRaw)
	if err != nil {
		return empty, ErrInvalid
	}
	snapshot, err := DecodePinnedSessionInputSnapshot(snapshotRef, snapshotRaw)
	if err != nil || plan.IntentSHA != owned.Intent.SHA256 || plan.ModelLockSHA != owned.ModelLockSHA || plan.Calibration != owned.Calibration || plan.SnapshotDigest != snapshot.SnapshotDigest || len(plan.Candidates) != len(snapshot.Items) || len(documents) != len(owned.Candidates) || len(documents) != len(plan.Candidates) {
		return empty, ErrInvalid
	}
	byTarget := map[uuid.UUID]SemanticCandidate{}
	for n, document := range documents {
		if len(document.Body) == 0 || len(document.Body) > 1<<20 {
			return empty, ErrInvalid
		}
		digest := sha256.Sum256(document.Body)
		if document.Ref != owned.Candidates[n] || hex.EncodeToString(digest[:]) != document.Ref.SHA256 {
			return empty, ErrInvalid
		}
		candidate, err := DecodeSemanticCandidate(document.Body)
		if err != nil {
			return empty, ErrInvalid
		}
		if _, exists := byTarget[candidate.Target.AssetVersionID]; exists {
			return empty, ErrInvalid
		}
		byTarget[candidate.Target.AssetVersionID] = candidate
	}
	for n, item := range snapshot.Items {
		expected, exists := byTarget[item.Binding.AssetVersionID]
		actual := plan.Candidates[n]
		if !exists || actual.Target != item.Binding {
			return empty, ErrInvalid
		}
		expectedSHA, err := canonical.Hash(expected)
		actualSHA, actualErr := canonical.Hash(actual)
		if err != nil || actualErr != nil || expectedSHA != actualSHA {
			return empty, ErrInvalid
		}
	}
	return plan, nil
}
