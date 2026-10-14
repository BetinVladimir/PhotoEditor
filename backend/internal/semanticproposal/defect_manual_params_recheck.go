package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Rechecks original manual closure after other preparation IO. Final admission
// still needs transaction locks; this method performs no publication or queue IO.
func (s SQLTransferSourceReader) recheckManualDefectParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, prepared defectManualParamsPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || prepared.manualCount < 0 || prepared.manualCount > 64 || len(prepared.params.params) < 2 || len(prepared.params.params) > 1048576 || len(prepared.params.roots) > 4194304 || len(prepared.params.inputs) > 4194304 {
		return v2input.ErrInvalid
	}
	source := prepared.source
	if source.tenant != tenant || source.project != s.ProjectID || source.actor != who.UserID || source.session != who.SessionID || source.binding != binding || source.bucket != s.Bucket || !source.unitRGB {
		return v2input.ErrInvalid
	}
	if _, err := marshalSourceEvidencePins(source); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(prepared.params.params))
	if prepared.params.digest != hex.EncodeToString(digest[:]) {
		return v2input.ErrInvalid
	}
	var roots, inputs []review.ArtifactRef
	if json.Unmarshal([]byte(prepared.params.roots), &roots) != nil || roots == nil || len(roots) > 64 || json.Unmarshal([]byte(prepared.params.inputs), &inputs) != nil || len(inputs) < 2 || len(inputs) > 4096 {
		return v2input.ErrInvalid
	}
	declared := map[uuid.UUID]string{}
	for _, input := range inputs {
		if input.ID == uuid.Nil || declared[input.ID] != "" {
			return v2input.ErrInvalid
		}
		declared[input.ID] = input.SHA256
	}
	required := map[v2input.Ref]bool{}
	for _, root := range roots {
		ref := v2input.Ref{ArtifactID: root.ID, SHA256: root.SHA256}
		if root.ID == uuid.Nil || declared[root.ID] != root.SHA256 || required[ref] {
			return v2input.ErrInvalid
		}
		required[ref] = true
	}
	seen := map[v2input.Ref]defectManualExecutionProof{}
	// Validate every retained closure before the first live catalog read.
	for i := 0; i < prepared.manualCount; i++ {
		owned := prepared.manual[i]
		proof := owned.original()
		if !validManualPreparationScope(s.ProjectID, who, tenant, binding, proof) || !required[proof.objects.Root.ref] || declared[proof.objects.Pixel.ref.ArtifactID] != proof.objects.Pixel.ref.SHA256 || declared[proof.children[0].ref.ArtifactID] != proof.children[0].ref.SHA256 {
			return v2input.ErrInvalid
		}
		if previous, ok := seen[proof.objects.Root.ref]; ok && previous != owned {
			return v2input.ErrInvalid
		}
		if _, err := marshalManualEvidencePins(proof, s.Bucket); err != nil {
			return err
		}
		seen[proof.objects.Root.ref] = owned
	}
	if len(seen) != len(required) {
		return v2input.ErrInvalid
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, source); err != nil {
		return err
	}
	for i := 0; i < prepared.manualCount; i++ {
		if err := s.recheckManualPreparationProof(ctx, who, tenant, binding, prepared.manual[i].original()); err != nil {
			return err
		}
	}
	return s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, source)
}
