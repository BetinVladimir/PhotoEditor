package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Validates retained exact catalog metadata and its complete frozen inventory
// before any SQL lock callback. Actual ready/expiry/auth checks require SQL.
func decodeDetectorInputEvidence(ctx context.Context, frozen v2input.FrozenSemanticDetectorTask, body string) ([]v2draft.InputEvidence, error) {
	return decodeNativeInputEvidence(ctx, frozen, body)
}

type nativeInputInventoryVerifier interface {
	VerifyInputInventory(context.Context, []v2input.Entry) error
}

func decodePersonInputEvidence(ctx context.Context, frozen v2input.FrozenSemanticPersonTask, body string) ([]v2draft.InputEvidence, error) {
	return decodeNativeInputEvidence(ctx, frozen, body)
}

// Both source-only native stages use the same scoped source child contracts.
// Each concrete frozen task independently verifies its operation-bound digest.
func decodeNativeInputEvidence(ctx context.Context, frozen nativeInputInventoryVerifier, body string) ([]v2draft.InputEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(body) < 2 || len(body) > 4<<20 {
		return nil, v2input.ErrInvalid
	}
	canonicalBody, err := canonical.Bytes([]byte(body))
	if err != nil || string(canonicalBody) != body {
		return nil, v2input.ErrInvalid
	}
	var evidence []v2draft.InputEvidence
	if json.Unmarshal(canonicalBody, &evidence) != nil || len(evidence) < 3 || len(evidence) > 4097 {
		return nil, v2input.ErrInvalid
	}
	entries := make([]v2input.Entry, len(evidence))
	seen := map[uuid.UUID]bool{}
	for i, entry := range evidence {
		obj := entry.Object
		if seen[entry.Ref.ArtifactID] || obj.SHA256 != entry.Ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.Bytes <= 0 || obj.Bytes > 9007199254740991 {
			return nil, v2input.ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
		contract := entry.Contract
		switch contract {
		case "WorkingPixels", "WorkingAlpha", "DenseWarpField":
			contract = "NpyArray"
		}
		entries[i] = v2input.Entry{Ref: entry.Ref, Contract: contract}
	}
	if err := frozen.VerifyInputInventory(ctx, entries); err != nil {
		return nil, err
	}
	return evidence, nil
}

// Matches every original input object to the source proof used for byte/ICC
// verification and SQL locks. The Params root has its own document lock.
func verifyDetectorSourceInputPins(ctx context.Context, prepared sourceDetectorPreparation) error {
	evidence, err := decodeDetectorInputEvidence(ctx, prepared.frozen, prepared.inputs)
	if err != nil {
		return err
	}
	return verifyNativeSourceInputPins(ctx, evidence, prepared.source)
}

// LRASPP must join the exact source objects used for physical byte/ICC proof,
// not merely matching artifact hashes from a separate catalog lookup.
func verifyPersonSourceInputPins(ctx context.Context, prepared catalogPersonPreparation, source sourcePreparationProof) error {
	evidence, err := decodePersonInputEvidence(ctx, prepared.frozen, prepared.inputs)
	if err != nil {
		return err
	}
	return verifyNativeSourceInputPins(ctx, evidence, source)
}

func verifyNativeSourceInputPins(ctx context.Context, evidence []v2draft.InputEvidence, proof sourcePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := decodeSourcePreparationPins(proof)
	if err != nil {
		return err
	}
	known := map[uuid.UUID]sourcePreparationPin{}
	for _, pin := range source {
		if old, exists := known[pin.Node.Ref.ArtifactID]; exists {
			if old.Node.Ref != pin.Node.Ref || old.Node.Contract != pin.Node.Contract || old.Object != pin.Object {
				return v2input.ErrConflict
			}
		}
		known[pin.Node.Ref.ArtifactID] = pin
	}
	count := 0
	for _, entry := range evidence {
		if entry.Contract == "Params" {
			continue
		}
		pin, exists := known[entry.Ref.ArtifactID]
		if !exists || pin.Node.Ref != entry.Ref || pin.Node.Contract != entry.Contract || pin.Object != entry.Object {
			return v2input.ErrConflict
		}
		count++
	}
	if count != len(known) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
