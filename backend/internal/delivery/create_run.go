package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// BeginRun belongs in a short authorized transaction before document publication.
// It creates no task or quota reservation and cannot activate work.
func (i CreateIntent) BeginRun(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID, pending CreatePending, bucket string) error {
	return i.beginRun(ctx, tx, who, tid, pending, bucket, false)
}

// BeginWatermarkSelectionRun creates only the parent shell from frozen
// selection evidence. No executable prepare plan or task is materialized.
func (i CreateIntent) BeginWatermarkSelectionRun(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID, pending CreatePending, bucket string) error {
	return i.beginRun(ctx, tx, who, tid, pending, bucket, true)
}
func (i CreateIntent) beginRun(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID, pending CreatePending, bucket string, selection bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || i.identity.Run == uuid.Nil || tid != i.tenant || who.UserID != i.actor || who.SessionID == uuid.Nil || len(bucket) < 3 || len(bucket) > 63 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	raw, err := canonical.Marshal(pending)
	if err != nil {
		return err
	}
	verified, err := i.decodePendingProjection(ctx, who.SessionID, raw, nil, false, selection)
	if err != nil {
		return err
	}
	var docs CreateDocuments
	if selection {
		docs, err = i.projectCreateDocuments(ctx, who.SessionID, verified.Catalog, verified.Items)
	} else {
		docs, err = i.CompileDocuments(ctx, who.SessionID, verified.Catalog, verified.Items)
	}
	if err != nil {
		return err
	}
	config, err := canonical.Marshal(struct {
		QueueClass    string    `json:"queue_class,omitempty"`
		SchemaVersion int       `json:"schema_version"`
		Project       uuid.UUID `json:"project_id"`
		Snapshot      uuid.UUID `json:"snapshot_id"`
		Workflow      string    `json:"workflow_type"`
		RequestSHA    string    `json:"request_sha"`
		PolicySHA     string    `json:"policy_sha"`
	}{verified.QueueClass, 1, i.request.ProjectID, i.request.SnapshotID, "delivery", i.requestSHA, docs.Policy.Ref.SHA256})
	if err != nil {
		return err
	}
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT photo.begin_v30_delivery_create_run_bound($1,$2,$3,$4,$5,$6,$7)`, tid, who.UserID, who.SessionID, i.key, i.requestSHA, string(config), bucket).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_runtime_ready($1)`, bucket).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return domain.NewError("DELIVERY_UNAVAILABLE", 503)
		}
		return domain.NewError("DELIVERY_CREATE_STALE", 409)
	}
	return nil
}
