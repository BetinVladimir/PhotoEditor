package delivery

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

func(i CreateIntent)promotionCatalog(ctx context.Context,tx ProofTransaction,who auth.Identity,items []v2input.SessionSnapshotItem)(CreateCatalog,error){return i.promotionCatalogMode(ctx,tx,who,items,false)}
func (i CreateIntent) promotionCatalogMode(ctx context.Context, tx ProofTransaction, who auth.Identity, items []v2input.SessionSnapshotItem,watermark bool) (CreateCatalog, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT response_body FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Delivery.Selection' AND key=$3 AND request_hash=$4 AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE`, i.tenant, i.actor, i.key, i.requestSHA).Scan(&raw); err != nil {
		return CreateCatalog{}, err
	}
	selection, err := i.decodeRecipeSelectionPending(ctx,raw,watermark)
	if err != nil {
		return CreateCatalog{}, err
	}
	if len(items) != len(selection.Pins) {
		return CreateCatalog{}, v2input.ErrInvalid
	}
	for index, pin := range selection.Pins {
		if items[index].Ordinal != index || items[index].Binding != pin.Binding || items[index].WorkingImage != pin.Binding.Source {
			return CreateCatalog{}, v2input.ErrInvalid
		}
		key := uuid.NewSHA1(i.identity.Run, []byte("delivery:recipe:"+strconv.Itoa(index)+":"+pin.Asset.String())).String()
		id, err := v2render.RenderFreezeDocumentID(i.tenant, who.UserID, pin.Asset, key, pin.Request)
		if err != nil {
			return CreateCatalog{}, err
		}
		if err := verifyCreateFreezeReceipt(ctx, tx, who, i.tenant, pin, key, id, items[index].Recipe); err != nil {
			return CreateCatalog{}, err
		}
		var exact bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.v2_draft_uploads WHERE tenant_id=$1 AND project_id=$2 AND asset_version_id=$3 AND document_id=$4 AND artifact_id=$5 AND sha256=$6 AND state='ready' AND render_registered_at IS NOT NULL)`, i.tenant, i.request.ProjectID, pin.Binding.AssetVersionID, id, items[index].Recipe.ArtifactID, items[index].Recipe.SHA256).Scan(&exact); err != nil {
			return CreateCatalog{}, err
		}
		if !exact {
			return CreateCatalog{}, domain.NewError("DELIVERY_RECIPE_SELECTION_STALE", 409)
		}
	}
	return selection.Catalog, nil
}

// A deterministic upload ID alone does not prove which draft revision was
// frozen. Bind it to the completed V26 request hash and exact saved response.
func verifyCreateFreezeReceipt(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID, pin CreateRecipePin, key string, id uuid.UUID, recipe v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hash, err := canonical.Hash(pin.Request)
	if err != nil {
		return err
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT response_body FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 AND request_hash=$5 AND response_status=200 AND resource_id=$6 AND expires_at>clock_timestamp() FOR SHARE`, tid, who.UserID, "Render.Freeze."+pin.Asset.String(), key, hash, id).Scan(&raw)
	if err == pgx.ErrNoRows {
		return domain.NewError("DELIVERY_RECIPE_SELECTION_STALE", 409)
	}
	if err != nil {
		return err
	}
	result, err := v2render.DecodeRenderFreezeResult(raw, id, pin.Request)
	if err != nil || result.Source != pin.Binding || result.Recipe != recipe {
		return domain.NewError("DELIVERY_RECIPE_SELECTION_STALE", 409)
	}
	return ctx.Err()
}

// PrepareCreatePending resumes complete pending before mutable observations;
// otherwise pins selection, freezes exact revisions outside transactions, then
// promotes under the original catalog and live recipe/source authority.
func (s Service) PrepareCreatePending(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, freezer CreateRecipeFreezer) (CreateRecovery, error) {
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return CreateRecovery{}, err
	}
	request = intent.Request()
	current, err := s.RecoverCreate(ctx, who, tid, request, key)
	if err != nil || current.Found {
		return current, err
	}
	if freezer == nil {
		return CreateRecovery{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	selection, err := s.PinCreateRecipeSelection(ctx, who, tid, request, key)
	if err != nil {
		return CreateRecovery{}, err
	}
	refs, err := intent.FreezeRecipeSelection(ctx, who, selection.Pins, freezer)
	if err != nil {
		return CreateRecovery{}, err
	}
	var items []v2input.SessionSnapshotItem
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		items, err = intent.ResolveSelection(ctx, tx, who, tid, refs)
		return err
	})
	if err != nil {
		return CreateRecovery{}, err
	}
	pending, completed, err := s.pinCreatePending(ctx, who, tid, request, key, selection.Catalog, items, true)
	if err != nil {
		return CreateRecovery{}, err
	}
	return CreateRecovery{Found: true, Completed: completed, Pending: pending}, nil
}

// PrepareWatermarkCreatePending freezes exact recipe revisions and promotes the
// original catalog to selection-only pending evidence. Pins/proof follow later.
func (s Service) PrepareWatermarkCreatePending(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, freezer CreateRecipeFreezer) (CreateRecovery, error) {
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return CreateRecovery{}, err
	}
	request = intent.Request()
	current, err := s.RecoverWatermarkSelection(ctx, who, tid, request, key)
	if err != nil || current.Found {
		return current, err
	}
	if freezer == nil {
		return CreateRecovery{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	selection, err := s.PinWatermarkRecipeSelection(ctx, who, tid, request, key)
	if err != nil {
		return CreateRecovery{}, err
	}
	refs, err := intent.FreezeRecipeSelection(ctx, who, selection.Pins, freezer)
	if err != nil {
		return CreateRecovery{}, err
	}
	var items []v2input.SessionSnapshotItem
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		items, err = intent.ResolveSelection(ctx, tx, who, tid, refs)
		return err
	})
	if err != nil {
		return CreateRecovery{}, err
	}
	pending, completed, err := s.pinCreatePendingProjection(ctx,who,tid,request,key,selection.Catalog,items,true,true)
	if err != nil {
		return CreateRecovery{}, err
	}
	return CreateRecovery{Found: true, Completed: completed, Pending: pending}, nil
}
