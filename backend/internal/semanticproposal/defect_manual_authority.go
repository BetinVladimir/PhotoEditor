package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type defectManualAuthorityEntry struct {
	Root     v2input.Ref     `json:"root"`
	Target   v2input.Ref     `json:"target"`
	Evidence json.RawMessage `json:"evidence"`
}

// Exact complete manual closures, including original optional external vectors,
// both target/protected arrays and preview PNGs. Evidence comes exclusively
// from privately retained proofs; source/producer SQL authority is separate.
func (s SQLTransferSourceReader) marshalDefectManualAuthority(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectMaskExecutionPreparation) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	proofs, err := s.decodeDefectExecutionManualProofs(ctx, who, tenant, binding, prepared)
	if err != nil {
		return nil, err
	}
	entries := make([]defectManualAuthorityEntry, 0, len(proofs))
	for _, proof := range proofs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		evidence, err := marshalManualEvidencePins(proof, s.Bucket)
		if err != nil {
			return nil, err
		}
		entries = append(entries, defectManualAuthorityEntry{Root: proof.objects.Root.ref, Target: proof.objects.Pixel.ref, Evidence: evidence})
	}
	body, err := canonical.Marshal(entries)
	if err != nil {
		return nil, err
	}
	if len(body) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}
