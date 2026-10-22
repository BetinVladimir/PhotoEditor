package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"time"
)

const createPendingMaxBytes = 64 << 20

// Private execution evidence stored in the existing pending idempotency row.
// It is never an HTTP response. The original actor scope survives session rotation.
type CreatePendingDocument struct {
	Purpose string      `json:"purpose"`
	Ref     v2input.Ref `json:"ref"`
	Body    string      `json:"body_jcs"`
}
type CreatePending struct {
	Stage string `json:"stage,omitempty"`
	QueueClass string `json:"queue_class,omitempty"`
	Project       uuid.UUID                     `json:"project_id"`
	SchemaVersion int                           `json:"schema_version"`
	Documents     []CreatePendingDocument       `json:"documents"`
	Tenant        uuid.UUID                     `json:"tenant_id"`
	Actor         uuid.UUID                     `json:"actor_id"`
	RequestSHA    string                        `json:"request_sha"`
	Catalog       CreateCatalog                 `json:"catalog"`
	Items         []v2input.SessionSnapshotItem `json:"items"`
}

func (i CreateIntent) EncodePending(ctx context.Context, session uuid.UUID, catalog CreateCatalog, items []v2input.SessionSnapshotItem) ([]byte, error) {
 return i.encodePending(ctx,session,catalog,items,nil,false)
}
func(i CreateIntent)EncodeWatermarkPending(ctx context.Context,session uuid.UUID,catalog CreateCatalog,items []v2input.SessionSnapshotItem,pins []PreparedWatermarkPin)([]byte,error){
 return i.encodePending(ctx,session,catalog,items,pins,true)
}
func(i CreateIntent)encodePending(ctx context.Context,session uuid.UUID,catalog CreateCatalog,items []v2input.SessionSnapshotItem,pins []PreparedWatermarkPin,watermark bool)([]byte,error){
	docs, err := i.compileDocuments(ctx, session, catalog, items,pins,watermark)
	if err != nil {
		return nil, err
	}
	stage := "ordinary"
	if watermark { stage = "watermark" }
	raw, err := canonical.Marshal(CreatePending{Stage: stage, QueueClass: "batch", Project: i.request.ProjectID, SchemaVersion: 1, Tenant: i.tenant, Actor: i.actor, RequestSHA: i.requestSHA, Catalog: catalog, Items: items, Documents: pendingCreateDocuments(docs)})
	if err != nil {
		return nil, err
	}
	if len(raw) > createPendingMaxBytes {
		return nil, domain.NewError("DELIVERY_PENDING_TOO_LARGE", 422)
	}
	return raw, nil
}
func (i CreateIntent) DecodePending(ctx context.Context, session uuid.UUID, raw []byte) (CreatePending, error) {
 return i.decodePending(ctx,session,raw,nil,false)
}
// Pins are independently loaded from the durable parent run and reauthorized;
// the pending envelope cannot select its own watermark source authority.
func(i CreateIntent)DecodeWatermarkPending(ctx context.Context,session uuid.UUID,raw []byte,pins []PreparedWatermarkPin)(CreatePending,error){
 return i.decodePending(ctx,session,raw,pins,true)
}
func(i CreateIntent)decodePending(ctx context.Context,session uuid.UUID,raw []byte,pins []PreparedWatermarkPin,watermark bool)(CreatePending,error){
 return i.decodePendingProjection(ctx,session,raw,pins,watermark,false)
}
func(i CreateIntent)decodePendingProjection(ctx context.Context,session uuid.UUID,raw []byte,pins []PreparedWatermarkPin,watermark,selection bool)(CreatePending,error){
	invalid := func() (CreatePending, error) {
		return CreatePending{}, domain.NewError("DELIVERY_PENDING_INVALID", 500)
	}
	if err := ctx.Err(); err != nil {
		return CreatePending{}, err
	}
	if len(raw) < 2 || len(raw) > 80<<20 {
		return invalid()
	}
	body, err := canonical.Bytes(raw)
	if err != nil || len(body) > createPendingMaxBytes {
		return invalid()
	}
	var pending CreatePending
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pending) != nil || pending.SchemaVersion != 1 || (pending.QueueClass != "" && pending.QueueClass != "batch") || pending.Project != i.request.ProjectID || pending.Tenant != i.tenant || pending.Actor != i.actor || pending.RequestSHA != i.requestSHA {
		return invalid()
	}
	// Legacy envelopes omitted this distinction. They may be restored only as
	// selection evidence, then independently revalidated and promoted under the
	// receipt lock; missing stage is never executable authority.
	if selection {
		if pending.Stage != "" && pending.Stage != "selection" { return invalid() }
	} else if watermark {
		if pending.Stage != "watermark" { return invalid() }
	} else if pending.Stage != "ordinary" { return invalid() }
	exact, err := canonical.Marshal(pending)
	if err != nil || !bytes.Equal(exact, body) {
		return invalid()
	}
 var docs CreateDocuments
 if selection{docs,err=i.projectCreateDocuments(ctx,session,pending.Catalog,pending.Items)}else{docs,err=i.compileDocuments(ctx,session,pending.Catalog,pending.Items,pins,watermark)}
	if err != nil {
		return invalid()
	}
	expected, err := canonical.Marshal(pendingCreateDocuments(docs))
	if err != nil {
		return invalid()
	}
	stored, err := canonical.Marshal(pending.Documents)
	if err != nil || !bytes.Equal(expected, stored) {
		return invalid()
	}
	return pending, nil
}

// PinCreatePending commits only execution evidence. It performs no object IO,
// creates no run and dispatches no work. Final admission must save the public
// JobTicket in this same scope atomically with graph activation.
func (s Service) PinCreatePending(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, catalog CreateCatalog, items []v2input.SessionSnapshotItem) (CreatePending, bool, error) {
	return s.pinCreatePending(ctx, who, tid, request, key, catalog, items, false)
}

func (s Service) pinCreatePending(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, catalog CreateCatalog, items []v2input.SessionSnapshotItem, fromSelection bool) (CreatePending, bool, error) {
 return s.pinCreatePendingProjection(ctx,who,tid,request,key,catalog,items,fromSelection,false)
}
// PinWatermarkSelectionPending persists only candidate selection evidence. All
// source pin preparation and executable proof admission follow this commit.
func(s Service)PinWatermarkSelectionPending(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,catalog CreateCatalog,items []v2input.SessionSnapshotItem)(CreatePending,bool,error){
 return s.pinCreatePendingProjection(ctx,who,tid,request,key,catalog,items,false,true)
}
func(s Service)pinCreatePendingProjection(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,catalog CreateCatalog,items []v2input.SessionSnapshotItem,fromSelection,watermarkSelection bool)(CreatePending,bool,error){
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return CreatePending{}, false, err
	}
	var payload []byte
 if watermarkSelection{payload,err=intent.EncodeWatermarkSelectionPending(ctx,who.SessionID,catalog,items)}else{payload,err=intent.EncodePending(ctx,who.SessionID,catalog,items)}
	if err != nil {
		return CreatePending{}, false, err
	}
	if s.DB == nil {
		return CreatePending{}, false, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	var pending CreatePending
	completed := false
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var authorized bool
		if err := tx.QueryRow(ctx, `SELECT photo.v2_editor_authorized($1,$2,$3,$4)`, tid, request.ProjectID, who.UserID, who.SessionID).Scan(&authorized); err != nil {
			return err
		}
		if !authorized {
			return domain.NewError("NOT_FOUND", 404)
		}
		const route = "POST /delivery-packages"
		if _, err := tx.Exec(ctx, `INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING`, tid, who.UserID, route, key, intent.requestSHA, time.Now().UTC().Add(24*time.Hour)); err != nil {
			return err
		}
		var hash string
		var status *int
		var saved []byte
		var live bool
		if err := tx.QueryRow(ctx, `SELECT request_hash,response_status,response_body,expires_at>clock_timestamp() FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 FOR UPDATE`, tid, who.UserID, route, key).Scan(&hash, &status, &saved, &live); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT photo.v2_editor_authorized($1,$2,$3,$4)`, tid, request.ProjectID, who.UserID, who.SessionID).Scan(&authorized); err != nil {
			return err
		}
		if !authorized {
			return domain.NewError("NOT_FOUND", 404)
		}
		if hash != intent.requestSHA {
			return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		}
		if status != nil {
			completed = true
			return nil
		}
		if !live {
			return domain.NewError("SNAPSHOT_EXPIRED", 410)
		}
		if len(saved) > 0 && string(saved) != "null" {
			var err error
			if watermarkSelection{pending,err=intent.DecodeWatermarkSelectionPending(ctx,who.SessionID,saved)}else{pending,err=intent.DecodePending(ctx,who.SessionID,saved)}
			if err != nil {
				return err
			}
		} else {
			// Fresh freeze must still match the catalog observation it was compiled from.
			var current CreateCatalog
			var err error
			if fromSelection {
				current, err = intent.promotionCatalogMode(ctx,tx,who,items,watermarkSelection)
			} else if watermarkSelection {
                current,err=intent.ResolveWatermarkCatalog(ctx,tx,who,tid)
            } else {
				current, err = intent.ResolveCatalog(ctx, tx, who, tid)
			}
			if err != nil {
				return err
			}
			currentRaw, err := canonical.Marshal(current)
			if err != nil {
				return err
			}
			wantedRaw, err := canonical.Marshal(catalog)
			if err != nil {
				return err
			}
			if !bytes.Equal(currentRaw, wantedRaw) {
				return domain.NewError("DELIVERY_SOURCE_STALE", 409)
			}
			if watermarkSelection{pending,err=intent.DecodeWatermarkSelectionPending(ctx,who.SessionID,payload)}else{pending,err=intent.DecodePending(ctx,who.SessionID,payload)}
			if err != nil {
				return err
			}
		}
		refs := make([]v2input.Ref, len(pending.Items))
		for index, item := range pending.Items {
			refs[index] = item.Recipe
		}
		current, err := intent.ResolveSelection(ctx, tx, who, tid, refs)
		if err != nil {
			return err
		}
		currentRaw, err := canonical.Marshal(current)
		if err != nil {
			return err
		}
		pinnedRaw, err := canonical.Marshal(pending.Items)
		if err != nil {
			return err
		}
		if !bytes.Equal(currentRaw, pinnedRaw) {
			return domain.NewError("DELIVERY_SOURCE_STALE", 409)
		}
		if len(saved) == 0 || string(saved) == "null" {
			_, err = tx.Exec(ctx, `UPDATE photo.idempotency_records SET response_body=$5 WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 AND response_status IS NULL`, tid, who.UserID, route, key, payload)
			return err
		}
		return nil
	})
	return pending, completed, err
}

func pendingCreateDocuments(docs CreateDocuments) []CreatePendingDocument {
	sequence := []CreateDocument{docs.Snapshot}
	sequence = append(sequence, docs.Presets...)
	sequence = append(sequence, docs.Policy, docs.Frozen, docs.Params)
	out := make([]CreatePendingDocument, 0, len(sequence))
	for _, doc := range sequence {
		out = append(out, CreatePendingDocument{Purpose: doc.Purpose, Ref: doc.Ref, Body: string(doc.Body)})
	}
	return out
}
