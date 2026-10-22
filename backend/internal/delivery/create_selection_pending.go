package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2render"
)

// CreateSelectionPending is private pre-IO evidence in the existing ledger.
// It is distinct from the later complete document/recipe CreatePending phase.
type CreateSelectionPending struct {
	SchemaVersion int               `json:"schema_version"`
	Tenant        uuid.UUID         `json:"tenant_id"`
	Actor         uuid.UUID         `json:"actor_id"`
	RequestSHA    string            `json:"request_sha"`
	Catalog       CreateCatalog     `json:"catalog"`
	Pins          []CreateRecipePin `json:"pins"`
}

func(i CreateIntent)decodeSelectionPending(ctx context.Context,raw []byte)(CreateSelectionPending,error){return i.decodeRecipeSelectionPending(ctx,raw,false)}
func (i CreateIntent) decodeRecipeSelectionPending(ctx context.Context, raw []byte,watermark bool) (CreateSelectionPending, error) {
	invalid := func() (CreateSelectionPending, error) {
		return CreateSelectionPending{}, domain.NewError("DELIVERY_SELECTION_PENDING_INVALID", 500)
	}
	if err := ctx.Err(); err != nil {
		return CreateSelectionPending{}, err
	}
	if len(raw) < 2 || len(raw) > 16<<20 {
		return invalid()
	}
	var saved CreateSelectionPending
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&saved) != nil || decoder.Decode(new(any)) != io.EOF || saved.SchemaVersion != 1 || saved.Tenant != i.tenant || saved.Actor != i.actor || saved.RequestSHA != i.requestSHA || saved.Catalog.Snapshot != i.request.SnapshotID || saved.Catalog.ItemCount != len(saved.Pins) || len(saved.Pins) < 1 || len(saved.Pins) > 5000 {
		return invalid()
	}
	// Reuse the catalog validator rather than trusting serialized config bytes.
	presets := make([]map[string]any, len(saved.Catalog.Presets))
	for index, preset := range saved.Catalog.Presets {
		presets[index] = map[string]any{"id": preset.ID, "revision": preset.Revision, "sha256": preset.SHA256, "config": json.RawMessage(preset.ConfigJCS)}
	}
	wire, err := canonical.Marshal(map[string]any{"snapshot_id": saved.Catalog.Snapshot, "snapshot_digest": saved.Catalog.SnapshotDigest, "item_count": saved.Catalog.ItemCount, "presets": presets})
	if err != nil {
		return invalid()
	}
	verified, err := i.decodeCatalog(wire,watermark)
	if err != nil {
		return invalid()
	}
	storedCatalog, err := canonical.Marshal(saved.Catalog)
	if err != nil {
		return invalid()
	}
	verifiedCatalog, err := canonical.Marshal(verified)
	if err != nil || !bytes.Equal(storedCatalog, verifiedCatalog) {
		return invalid()
	}
	seen := make(map[uuid.UUID]bool)
	for index, pin := range saved.Pins {
		if err := ctx.Err(); err != nil {
			return CreateSelectionPending{}, err
		}
		if pin.Ordinal != index || pin.Binding.AssetVersionID == uuid.Nil || seen[pin.Binding.AssetVersionID] || pin.Binding.Source.ArtifactID == uuid.Nil || !shaPattern.MatchString(pin.Binding.Source.SHA256) || !shaPattern.MatchString(pin.Binding.CanvasHash) || pin.Request.AssumeProfile != nil {
			return invalid()
		}
		seen[pin.Binding.AssetVersionID] = true
		if _, err := v2render.RenderFreezeDocumentID(i.tenant, i.actor, pin.Asset, "selection-validation", pin.Request); err != nil {
			return invalid()
		}
	}
	return saved, nil
}

// PinCreateRecipeSelection must precede all recipe object IO. The separate
// internal route uses the same actor/key/request hash and survives partial child
// freezing without replacing observed revisions or preset configurations.
func(s Service)PinCreateRecipeSelection(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(CreateSelectionPending,error){return s.pinRecipeSelection(ctx,who,tid,request,key,false)}
func(s Service)PinWatermarkRecipeSelection(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(CreateSelectionPending,error){return s.pinRecipeSelection(ctx,who,tid,request,key,true)}
func (s Service) pinRecipeSelection(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string,watermark bool) (CreateSelectionPending, error) {
	if err := ctx.Err(); err != nil {
		return CreateSelectionPending{}, err
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return CreateSelectionPending{}, err
	}
	if s.DB == nil {
		return CreateSelectionPending{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	var result CreateSelectionPending
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var authorized bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_lens_snapshot_ready($1,$2,$3,$4,$5) AND EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND download_allowed)`, tid, intent.request.ProjectID, intent.request.SnapshotID, who.UserID, who.SessionID).Scan(&authorized); err != nil {
			return err
		}
		if !authorized {
			return domain.NewError("DELIVERY_SOURCE_STALE", 409)
		}
		const route = "Delivery.Selection"
		if _, err := tx.Exec(ctx, `INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING`, tid, who.UserID, route, key, intent.requestSHA, time.Now().UTC().Add(24*time.Hour)); err != nil {
			return err
		}
		var hash string
		var body []byte
		var live bool
		var status *int
		if err := tx.QueryRow(ctx, `SELECT request_hash,response_body,expires_at>clock_timestamp(),response_status FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 FOR UPDATE`, tid, who.UserID, route, key).Scan(&hash, &body, &live, &status); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT photo.v25_lens_snapshot_ready($1,$2,$3,$4,$5) AND EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND download_allowed)`, tid, intent.request.ProjectID, intent.request.SnapshotID, who.UserID, who.SessionID).Scan(&authorized); err != nil {
			return err
		}
		if !authorized {
			return domain.NewError("DELIVERY_SOURCE_STALE", 409)
		}
		if hash != intent.requestSHA {
			return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		}
		if !live {
			return domain.NewError("SNAPSHOT_EXPIRED", 410)
		}
		if status != nil {
			return domain.NewError("DELIVERY_SELECTION_PENDING_INVALID", 500)
		}
		if len(body) > 0 && !bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
			var err error
			result, err = intent.decodeRecipeSelectionPending(ctx, body,watermark)
			return err
		}
		catalog, err := intent.resolveCatalog(ctx, tx, who, tid,watermark)
		if err != nil {
			return err
		}
		pins, err := intent.CaptureDraftSelection(ctx, tx, who, tid)
		if err != nil {
			return err
		}
		body, err = canonical.Marshal(CreateSelectionPending{SchemaVersion: 1, Tenant: tid, Actor: who.UserID, RequestSHA: intent.requestSHA, Catalog: catalog, Pins: pins})
		if err != nil {
			return err
		}
		result, err = intent.decodeRecipeSelectionPending(ctx, body,watermark)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE photo.idempotency_records SET response_body=$5::jsonb WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 AND response_status IS NULL`, tid, who.UserID, route, key, string(body))
		return err
	})
	if err != nil {
		return CreateSelectionPending{}, err
	}
	return result, nil
}
