package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"time"
)

type PerspectiveReceipt struct {
	SchemaVersion int                         `json:"schema_version"`
	Run           uuid.UUID                   `json:"run_id"`
	Workflow      uuid.UUID                   `json:"workflow_id"`
	Task          uuid.UUID                   `json:"task_id"`
	State         string                      `json:"state"`
	Actor         uuid.UUID                   `json:"actor_id"`
	Session       uuid.UUID                   `json:"auth_session_id"`
	Operation     string                      `json:"operation"`
	DescriptorSHA string                      `json:"descriptor_sha"`
	Qualification PerspectiveQualificationPin `json:"qualification"`
	Transport     json.RawMessage             `json:"transport"`
}

func parsePerspectiveReceipt(ctx context.Context, prepared PreparedPerspective, raw []byte) (PerspectiveReceipt, error) {
	var zero PerspectiveReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var receipt PerspectiveReceipt
	if json.Unmarshal(normal, &receipt) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(receipt)
	if err != nil || !bytes.Equal(normal, exact) {
		return zero, v2input.ErrInvalid
	}
	descriptor, err := prepared.Descriptor(ctx)
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(json.RawMessage(descriptor))
	if err != nil {
		return zero, err
	}
	s, err := prepared.draft.Snapshot()
	if err != nil {
		return zero, err
	}
	qualified, err := prepared.qualification.Qualification()
	if err != nil {
		return zero, err
	}
	pin, err := qualified.Pin()
	if err != nil {
		return zero, err
	}
	if receipt.SchemaVersion != 1 || receipt.State != "queued" || receipt.Run != s.Run || receipt.Workflow != s.Workflow || receipt.Task != s.Task || receipt.Actor != s.Actor || receipt.Session != s.Session || receipt.Operation != "optical.perspective" || receipt.DescriptorSHA != digest || receipt.Qualification != pin {
		return zero, v2input.ErrInvalid
	}
	if _, err = prepared.RestorePublishedTransport(ctx, receipt.Transport); err != nil {
		return zero, err
	}
	receipt.Transport = append(json.RawMessage(nil), receipt.Transport...)
	return receipt, nil
}

// ReadQueuedReceipt returns historical acknowledgement, not current task status.
func (c PerspectiveCoordinator) ReadQueuedReceipt(ctx context.Context, who auth.Identity, request PerspectivePrepareRequest) (PerspectiveReceipt, error) {
	var zero PerspectiveReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Reader == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || request.Tenant == uuid.Nil || request.Project == uuid.Nil || request.Snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(request.Key) {
		return zero, v2input.ErrInvalid
	}
	request.Params = append([]byte(nil), request.Params...)
	request.Source = append([]byte(nil), request.Source...)
	params, err := ParsePerspectiveParams(request.Params)
	if err != nil {
		return zero, err
	}
	image, err := VerifyPerspectiveWorkingImage(params, request.Source)
	if err != nil {
		return zero, err
	}
	// Reject unsupported modes and malformed/under-provisioned requests before IO.
	bounds, originals, err := c.prepareGeometry(ctx, who, request, params, image.Canvas.Width, image.Canvas.Height)
	if err != nil {
		return zero, err
	}
	required, err := PerspectiveResources(image.Canvas.Width, image.Canvas.Height, bounds.Width, bounds.Height)
	if err != nil {
		return zero, err
	}
	if err = ValidatePerspectiveCapacity(required, c.Capacity); err != nil {
		return zero, err
	}
	located, err := LoadPerspectiveQualificationEvidence(ctx, SQLPerspectiveQualificationCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, who, request.Tenant, request.Project, c.Pin, c.Locks, params, image.Color.ICCSHA256)
	if err != nil {
		return zero, err
	}
	qualified, err := located.Qualification()
	if err != nil {
		return zero, err
	}
	draft, err := CompilePerspectiveDraft(ctx, who, request.Tenant, request.Project, request.Snapshot, request.Key, request.Params, request.Source, qualified, c.MaxPixels, c.Capacity, originals...)
	if err != nil {
		return zero, err
	}
	if err = VerifyPerspectiveSourceObjects(ctx, SQLPerspectiveSourceArrayCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, who, request.Tenant, request.Project, request.Snapshot, params, request.Source); err != nil {
		return zero, err
	}
	snapshot, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}
	prepared := PreparedPerspective{draft: draft, params: artifactjson.Ref{ID: snapshot.ParamsDocument, SHA256: snapshot.ParamsSHA}, qualification: located}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = (perspectiveParamsRepository{}).authorize(ctx, tx, artifactjson.Scope{Tenant: request.Tenant}); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.read_v29_perspective_queued_receipt($1,$2,$3,$4,$5::jsonb)", request.Tenant, snapshot.Run, who.UserID, who.SessionID, draft.raw).Scan(&raw); err != nil {
		return zero, err
	}
	receipt, err := parsePerspectiveReceipt(ctx, prepared, raw)
	if err != nil {
		return zero, err
	}
	if err = LockLocatedPerspectiveQualification(ctx, tx, who, request.Tenant, request.Project, located); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return receipt, nil
}
