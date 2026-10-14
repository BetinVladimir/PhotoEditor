package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Locks the original source closure and defect qualification retained by execution
// preparation. Caller must roll back on failure and compose remaining producer,
// declared-file, inventory and authorization locks before admission. No object IO.
func (s SQLTransferSourceReader) lockDefectExecutionSourceInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectSourceExecutionPreparation) error {
	if tx == nil {
		return v2input.ErrInvalid
	}
	if _, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, prepared); err != nil {
		return err
	}
	var expected v2qualification.Expected
	if err := json.Unmarshal([]byte(prepared.expected), &expected); err != nil {
		return v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	if err := s.lockSourcePreparationProof(ctx, tx, who, tenant, binding, prepared.source); err != nil {
		return err
	}
	if err := s.lockDefectQualificationPreparation(ctx, tx, who, tenant, prepared.qualification, expected); err != nil {
		return err
	}
	return ctx.Err()
}

// Composes selected source and original face producer locks only. Remaining
// parser/manual/file/inventory/auth barriers and atomic admission are required.
func (s SQLTransferSourceReader) lockDefectExecutionFacesInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectFaceExecutionPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || prepared.producerCount < 0 || prepared.producerCount > len(prepared.producers) {
		return v2input.ErrInvalid
	}

	files, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, prepared.source)
	if err != nil {
		return err
	}
	landmarks := map[v2input.Ref]bool{}
	for _, file := range files {
		if file.Contract == "FaceLandmarks" {
			landmarks[file.Ref] = false
		}
	}
	if len(landmarks) != prepared.producerCount {
		return v2input.ErrInvalid
	}
	proofs := prepared.producers[:prepared.producerCount]

	for _, proof := range proofs {
		matched, exists := landmarks[proof.objects[0][0].ref]
		if !exists || matched {
			return v2input.ErrInvalid
		}
		landmarks[proof.objects[0][0].ref] = true
		for _, file := range files {
			if file.Ref == proof.objects[0][0].ref && file.Object != proof.objects[0][0].object {
				return v2input.ErrConflict
			}
		}
		if !validFacePreparationProofScope(s, who, tenant, binding, proof) || proof.expected != prepared.expected || proof.qualification != prepared.qualification {
			return v2input.ErrInvalid
		}
		if _, err := marshalFaceEvidencePins(proof); err != nil {
			return err
		}
		if _, _, err := marshalFaceQualificationPins(proof); err != nil {
			return err
		}
	}
	if err := s.lockDefectExecutionSourceInTransaction(ctx, tx, who, tenant, binding, prepared.source); err != nil {
		return err
	}
	if len(proofs) > 0 {
		if err := s.lockFacePreparationProofs(ctx, tx, who, tenant, binding, proofs); err != nil {
			return err
		}
	}
	return s.lockDefectExecutionSourceInTransaction(ctx, tx, who, tenant, binding, prepared.source)
}

// Locks selected original parser/label evidence around face/source locks.
// Manual roots, mask derivation outputs, other files and admission remain separate.
func (s SQLTransferSourceReader) lockDefectExecutionParsersInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectMaskExecutionPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || prepared.parserCount < 0 || prepared.parserCount > len(prepared.parsers) || len(prepared.expected) == 0 || len(prepared.expected) > 4<<20 || len(prepared.roots) == 0 || len(prepared.roots) > 4<<20 {
		return v2input.ErrInvalid
	}
	var expected [4]v2qualification.Expected
	if json.Unmarshal([]byte(prepared.expected), &expected) != nil {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(expected)
	if err != nil || string(body) != prepared.expected {
		return v2input.ErrInvalid
	}
	faceExpected, err := canonical.Marshal([2]v2qualification.Expected{expected[0], expected[1]})
	if err != nil || string(faceExpected) != prepared.face.expected || prepared.qualification[0] != prepared.face.qualification[0] || prepared.qualification[1] != prepared.face.qualification[1] {
		return v2input.ErrInvalid
	}
	parserExpected, err := canonical.Marshal([2]v2qualification.Expected{expected[2], expected[3]})
	if err != nil {
		return v2input.ErrInvalid
	}
	var roots []review.ProposalFaceMaskRoot
	if json.Unmarshal([]byte(prepared.roots), &roots) != nil {
		return v2input.ErrInvalid
	}
	body, err = canonical.Marshal(roots)
	if err != nil || string(body) != prepared.roots {
		return v2input.ErrInvalid
	}
	skinRoots := 0
	for _, root := range roots {
		if root.Binding.Mask.Selector == "skin" {
			skinRoots++
		}
	}
	if (skinRoots == 0) != (prepared.parserCount == 0) || prepared.parserCount > skinRoots {
		return v2input.ErrInvalid
	}
	proofs := prepared.parsers[:prepared.parserCount]
	seen := map[v2input.Ref]bool{}
	for _, proof := range proofs {
		if !validFaceParsePreparationProofScope(s, who, tenant, binding, proof) || proof.evidence.expected != string(parserExpected) || proof.evidence.qualification != [2]personEvidenceObject{prepared.qualification[2], prepared.qualification[3]} || seen[proof.evidence.objects[0][0].ref] {
			return v2input.ErrInvalid
		}
		seen[proof.evidence.objects[0][0].ref] = true
		if _, err := marshalFaceParseEvidencePins(proof); err != nil {
			return err
		}
		if _, _, err := marshalSelectedFaceQualificationPins(proof.evidence, [2]string{"face.parse", "face.detect"}); err != nil {
			return err
		}
	}
	if err := s.lockDefectExecutionFacesInTransaction(ctx, tx, who, tenant, binding, prepared.face); err != nil {
		return err
	}
	if len(proofs) > 0 {
		if err := s.lockFaceParsePreparationProofs(ctx, tx, who, tenant, binding, proofs); err != nil {
			return err
		}
	}
	return s.lockDefectExecutionFacesInTransaction(ctx, tx, who, tenant, binding, prepared.face)
}
