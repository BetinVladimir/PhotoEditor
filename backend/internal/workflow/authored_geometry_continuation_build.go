package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// This opt-in is completion-only: it grants no model/qualification/worker activation.
func parseAuthoredGeometryContinuationTenants(raw string) ([]uuid.UUID, error) {
	if len(raw) < 2 || len(raw) > 16384 {
		return nil, v2input.ErrInvalid
	}
	var ids []uuid.UUID
	d := json.NewDecoder(bytes.NewBufferString(raw))
	if d.Decode(&ids) != nil || d.Decode(new(any)) != io.EOF || len(ids) < 1 || len(ids) > 256 {
		return nil, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			return nil, v2input.ErrInvalid
		}
		seen[id] = true
	}
	return append([]uuid.UUID(nil), ids...), nil
}
func buildAuthoredGeometryContinuation(ctx context.Context, pool *pgxpool.Pool, objects *objectstore.Store, bucket string) (*AuthoredGeometryContinuationProcessor, error) {
	raw := os.Getenv("PHOTO_AUTHORED_GEOMETRY_CONTINUATION_TENANTS")
	if raw == "" {
		return nil, nil
	}
	ids, e := parseAuthoredGeometryContinuationTenants(raw)
	if e != nil {
		return nil, e
	}
	if pool == nil || objects == nil || bucket == "" {
		return nil, v2input.ErrInvalid
	}
	if e = database.RequireApplicationRole(ctx, pool, "photo_scheduler"); e != nil {
		return nil, e
	}
	// No unknown installed function is accepted by name alone. Source-generated pins
	// attest our exact read/discovery functions; owning 02611/12 still guard their dependencies.
	for _, pin := range authoredGeometryContinuationBodyPins {
		var valid bool
		e = pool.QueryRow(ctx, `SELECT coalesce((SELECT p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[] AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')=$2 AND has_function_privilege(current_user,p.oid,'EXECUTE') FROM pg_proc p WHERE p.oid=to_regprocedure($1)),false)`, pin.signature, pin.sha).Scan(&valid)
		if e != nil {
			return nil, e
		}
		if !valid {
			return nil, v2input.ErrExpired
		}
	}
	return &AuthoredGeometryContinuationProcessor{DB: pool, Reader: v2render.AuthoredGeometryProducerReader{Scheduler: pool, Objects: objects, Bucket: bucket}, tenants: ids, after: map[uuid.UUID]uuid.UUID{}}, nil
}
