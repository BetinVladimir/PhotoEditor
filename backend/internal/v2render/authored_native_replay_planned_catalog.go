package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Lookup-only original traversal deliberately has no authored geometry provider.
// It must initialize the original source under ordinary recipe contracts first.
type authoredReplayOriginalCatalog struct {
	source SQLAuthoredNativeReplayCatalog
}

func (s authoredReplayOriginalCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	return s.source.Lookup(ctx, who, tenant, node)
}

func (s SQLAuthoredNativeReplayCatalog) plannedInventory(ctx context.Context, who auth.Identity, tenant uuid.UUID) (AuthoredFinalInventory, error) {
	var raw []byte
	empty := AuthoredFinalInventory{}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if s.Scheduler == nil || tenant != s.request.Consumer.Tenant || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	err := database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
		var role string
		if e := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); e != nil {
			return e
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT photo.authored_native_replay_planned_prefix($1,$2,$3,$4)`, tenant, s.raw, who.UserID, who.SessionID).Scan(&raw)
	})
	if err != nil {
		return empty, err
	}
	return decodeAuthoredReplayPlannedInventory(ctx, raw, s.request, who)
}
func NewScopedAuthoredNativeReplayCatalog(ctx context.Context, who auth.Identity, db *pgxpool.Pool, objects v2input.SessionAnalysisObjectReader, bucket string, raw []byte) (SQLAuthoredNativeReplayCatalog, error) {
	s, err := NewAuthoredNativeReplayCatalog(db, objects, bucket, raw)
	if err != nil {
		return s, err
	}
	tenant := s.request.Consumer.Tenant
	before, err := s.plannedInventory(ctx, who, tenant)
	if err != nil {
		return SQLAuthoredNativeReplayCatalog{}, err
	}
	anchor, err := s.originalBinding(ctx, who, tenant)
	if err != nil {
		return SQLAuthoredNativeReplayCatalog{}, err
	}
	recipe, err := s.read(ctx, who, tenant, before.Context.OriginalRecipe)
	if err != nil {
		return SQLAuthoredNativeReplayCatalog{}, err
	}
	image, err := s.read(ctx, who, tenant, anchor.Source)
	if err != nil {
		return SQLAuthoredNativeReplayCatalog{}, err
	}
	resolver := v2draft.ClosureResolver{Catalog: authoredReplayOriginalCatalog{source: s}, Objects: objects}
	entries, err := ResolveRenderInputs(ctx, who, tenant, anchor, before.Context.OriginalRecipe, recipe, image, resolver)
	if err != nil {
		return SQLAuthoredNativeReplayCatalog{}, err
	}
	s.originalEntries = make(map[v2input.Entry]bool, len(entries))
	for _, entry := range entries {
		s.originalEntries[entry] = true
	}
	after, err := s.plannedInventory(ctx, who, tenant)
	if err != nil {
		return SQLAuthoredNativeReplayCatalog{}, err
	}
	a, e := canonical.Marshal(before)
	b, f := canonical.Marshal(after)
	if e != nil || f != nil || !bytes.Equal(a, b) {
		return SQLAuthoredNativeReplayCatalog{}, v2input.ErrConflict
	}
	return s, nil
}
