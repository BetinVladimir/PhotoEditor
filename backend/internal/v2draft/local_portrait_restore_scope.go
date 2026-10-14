package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type LocalPortraitRecoveryProof struct {
	Root           v2input.Ref
	RootRaw        []byte
	OriginalParams v2input.Ref
	OriginalRaw    []byte
	VectorsRaw     []byte
}

// RestoreVerifiedLocalPortraitTask restores compiler intent and repeats every
// current scoped catalog occurrence. Saved inventory is never used as a grant.
// Durable owner/fence admission and binary replay are still separate gates.
func (s ClosureResolver) RestoreVerifiedLocalPortraitTask(ctx context.Context, who auth.Identity,
	tenant, workflow uuid.UUID, ordinal int, savedRaw, sourceRaw []byte, runtimeSHA string,
	capacity v2portrait.LocalResourceEstimate, proof LocalPortraitRecoveryProof) (FrozenLocalPortraitTask, error) {
	var zero FrozenLocalPortraitTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	// Own caller buffers before catalog callbacks or object IO.
	sourceRaw = append([]byte(nil), sourceRaw...)
	proof.RootRaw = append([]byte(nil), proof.RootRaw...)
	proof.OriginalRaw = append([]byte(nil), proof.OriginalRaw...)
	proof.VectorsRaw = append([]byte(nil), proof.VectorsRaw...)
	frozen, err := RestoreLocalPortraitTask(ctx, workflow, ordinal, append([]byte(nil), savedRaw...), sourceRaw, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	var target, guard v2input.Ref
	switch frozen.draft.Operation {
	case "portrait.hair":
		var params v2portrait.HairParams
		if err = json.Unmarshal(frozen.params, &params); err != nil {
			return zero, err
		}
		target, guard = params.Mask, params.Protected
	case "portrait.clothing":
		var params v2portrait.ClothingParams
		if err = json.Unmarshal(frozen.params, &params); err != nil {
			return zero, err
		}
		target, guard = params.Mask, params.Protected
	default:
		return zero, v2input.ErrInvalid
	}
	evidence, err := s.ResolveLocalPortraitTaskInputEvidence(ctx, who, tenant, frozen.binding,
		v2input.Ref{ArtifactID: frozen.draft.ParamsArtifactID, SHA256: frozen.draft.ParamsSHA}, frozen.params, sourceRaw,
		proof.Root, proof.RootRaw, proof.OriginalParams, proof.OriginalRaw, proof.VectorsRaw, target, guard)
	if err != nil {
		return zero, err
	}
	if len(evidence.Entries) != len(frozen.entries) {
		return zero, v2input.ErrInvalid
	}
	for i, entry := range evidence.Entries {
		if entry != frozen.entries[i] {
			return zero, v2input.ErrInvalid
		}
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return frozen, nil
}
