package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Retains qualification and protection of one original native parent. Targets,
// source pixel authority and transfer admission remain separate obligations.
type nativeReferenceContinuationProof struct {
	defect     nativeDefectQualificationProof
	protection nativeReferenceProtectionProof
}

func composeNativeReferenceContinuation(defect nativeDefectQualificationProof, protection nativeReferenceProtectionProof) (nativeReferenceContinuationProof, error) {
	proof := nativeReferenceContinuationProof{defect: defect, protection: protection}
	if err := matchNativeReferenceContinuation(proof); err != nil {
		return nativeReferenceContinuationProof{}, err
	}
	proof.defect.faces = append([]facePreparationProof(nil), defect.faces...)
	proof.defect.parsers = append([]faceParsePreparationProof(nil), defect.parsers...)
	return proof, nil
}

func matchNativeReferenceContinuation(proof nativeReferenceContinuationProof) error {
	d, p := proof.defect, proof.protection
	if !d.verified || d.parent.anchor == "" || d.parent.result.task == uuid.Nil || d.parent.result.catalog.ResultID == uuid.Nil || d.parent != p.selection.registered.parent || p.selection.registered != p.person.registered || d.policy != p.policy || p.selection.count <= 0 || d.faceCount != len(d.faces) || d.parserCount != len(d.parsers) {
		return v2input.ErrInvalid
	}
	faces, parsers, err := nativeQualifiedProducerSets(d.faces, d.parsers)
	if err != nil || faces != d.faceSet || parsers != d.parserSet {
		return v2input.ErrInvalid
	}
	if err := matchNativeFaceProducerReports(d.reports, d.faces, d.parsers); err != nil {
		return err
	}
	sha, err := canonical.Hash(json.RawMessage(d.policy.body))
	if err != nil || sha != p.policySHA {
		return v2input.ErrConflict
	}
	return matchNativeReferencePolicy(p)
}

// Both independent components lock in one caller-owned transaction. No object
// IO or transaction commit occurs here; any refusal requires complete rollback.
func (s SQLTransferSourceReader) lockNativeReferenceContinuation(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeReferenceContinuationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	if err := matchNativeReferenceContinuation(proof); err != nil {
		return err
	}
	if err := s.lockQualifiedNativeDefectResult(ctx, tx, who, request, proof.defect); err != nil {
		return err
	}
	return s.lockNativeReferenceProtection(ctx, tx, who, request, proof.protection)
}
