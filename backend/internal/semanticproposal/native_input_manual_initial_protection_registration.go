package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Complete physical packet is built before SQL; registration is insert-only
// exact replay. This does not grant parent graph admission or candidate approval.
func (s SQLTransferSourceReader) registerNativeInputManualInitialProtection(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, published publishedNativeInputManualInitialProtection, color TransferColorProfile, built v2input.Built) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := published.preparation
	if scheduler == nil {
		return v2input.ErrInvalid
	}
	if err := verifyNativeInputManualInitialDraftSeal(ctx, p.plan); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(p.body))
	if published.root.ArtifactID == uuid.Nil || published.root.ArtifactID == p.pixels.ArtifactID || published.root.SHA256 != hex.EncodeToString(sum[:]) || p.pixels != p.result.evidence.Protection {
		return v2input.ErrInvalid
	}
	packet, err := s.marshalNativeInputManualInitialCompletedEvidence(ctx, who, p.plan, color, p.result, built)
	if err != nil {
		return err
	}
	parent := p.plan.prepared.stage.dependencies.dependencies.producers.detector
	r := parent.request
	anchor, err := canonical.Marshal(map[string]any{"parent_run_id": r.RunID, "request_sha": r.RequestSHA, "auth_session_id": who.SessionID, "source": r.Intent.Reference, "intent": parent.published.Ref, "snapshot": parent.anchor.snapshotRef})
	if err != nil {
		return err
	}
	return database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		if err := s.lockNativeCompletedManualInitialDefectInTransaction(ctx, tx, who, p.plan, packet); err != nil {
			return err
		}
		b := r.Intent.Reference
		var ready bool
		if err := tx.QueryRow(ctx, "SELECT photo.register_semantic_native_manual_input_protection($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14,$15,$16::jsonb)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, p.plan.shell.Tasks[0].ID, p.result.catalog.ResultID, string(anchor), p.document, published.root.ArtifactID, published.root.SHA256, p.body, string(p.result.evidence.Report), string(packet)).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
		return ctx.Err()
	})
}
