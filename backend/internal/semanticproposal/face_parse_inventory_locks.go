package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Locks every selected proof before the tenant barrier and repeats discovery
// under that same barrier. Completeness covers only one selected Expected policy
// pair; global policy discovery/source/auth/version admission remain separate.
func (s SQLTransferSourceReader) lockFaceParsePreparationInventory(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proofs []faceParsePreparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || len(proofs) < 1 || len(proofs) > 64 {
		return v2input.ErrInvalid
	}
	owned := append([]faceParsePreparationProof(nil), proofs...)
	body, expected, err := s.marshalFaceParsePreparationInventory(who, tenant, binding, owned)
	if err != nil {
		return err
	}
	if err := s.lockFaceParsePreparationProofs(ctx, tx, who, tenant, binding, owned); err != nil {
		return err
	}
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_parse_inventory($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, expected, string(body)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return nil
}

func (s SQLTransferSourceReader) marshalFaceParsePreparationInventory(who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proofs []faceParsePreparationProof) ([]byte, string, error) {
	if len(proofs) < 1 || len(proofs) > 64 {
		return nil, "", v2input.ErrInvalid
	}
	type item struct {
		Parse     v2input.Ref `json:"parse"`
		Pixels    v2input.Ref `json:"pixels"`
		Detection v2input.Ref `json:"detection"`
	}
	entries := make([]item, 0, len(proofs))
	seen := map[uuid.UUID]bool{}
	expected := proofs[0].evidence.expected
	for _, proof := range proofs {
		if !validFaceParsePreparationProofScope(s, who, tenant, binding, proof) {
			return nil, "", v2input.ErrInvalid
		}
		if _, _, err := marshalSelectedFaceQualificationPins(proof.evidence, [2]string{"face.parse", "face.detect"}); err != nil {
			return nil, "", err
		}
		if proof.evidence.expected != expected {
			return nil, "", v2input.ErrConflict
		}
		root := proof.evidence.objects[0][0].ref
		if seen[root.ArtifactID] {
			return nil, "", v2input.ErrConflict
		}
		seen[root.ArtifactID] = true
		entries = append(entries, item{Parse: root, Pixels: proof.pixels.ref, Detection: proof.evidence.objects[1][0].ref})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Parse.ArtifactID.String() < entries[j].Parse.ArtifactID.String()
	})
	body, err := canonical.Marshal(entries)
	return body, expected, err
}
