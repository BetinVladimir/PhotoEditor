package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// SQL-only reference component for all-native admission. Caller holds original
// parent advisory lock and rolls back every refusal. Physical reference content,
// selection separation, coverage and completed packet precede this transaction.
// Final target/qualification/graph locks remain independent and mandatory.
func (s SQLTransferSourceReader) lockNativeAllTargetTransferReference(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, reference nativeInputTransferReferenceDocuments, c TransferPreparation, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !reference.person.originalPinned {
		return v2input.ErrInvalid
	}
	if err := s.validateNativeInputTransferReference(ctx, who, request, c, reference); err != nil {
		return err
	}
	registered := reference.person.source.protection
	original := registered.published.preparation.plan.prepared.dependencies.preparation.person
	proof := reference.person.person
	if original == nil || reference.person.policy != original.policy || proof.count < 0 || proof.root != original.root || proof.pixels != original.pixels || proof.report != original.report || proof.expected != original.expected || proof.evidence != original.evidence || proof.qualification != original.qualification {
		return v2input.ErrConflict
	}
	if err := s.lockRegisteredNativeOriginalProtection(ctx, tx, who, registered, completed); err != nil {
		return fmt.Errorf("original protection: %w", err)
	}
	if err := s.lockSourcePreparationProof(ctx, tx, who, request.TenantID, request.Intent.Reference, reference.person.source.source); err != nil {
		return fmt.Errorf("original source: %w", err)
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, reference.person.policy.body).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("original policy/selection SQL barrier: %w", v2input.ErrConflict)
	}
	pins, err := s.marshalNativeInputSelectionPins(who, request, reference.selection)
	if err != nil {
		return err
	}
	b := request.Intent.Reference
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_selection_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, b.CanvasHash, reference.selection.root.ArtifactID, reference.selection.root.SHA256, request.Intent.TargetSnapshotID, string(pins)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("original policy/selection SQL barrier: %w", v2input.ErrConflict)
	}
	return ctx.Err()
}
