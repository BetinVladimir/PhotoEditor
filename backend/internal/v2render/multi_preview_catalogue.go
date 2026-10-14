package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

// SQLMultiPreviewCatalogue is bound to one scheduler observation. Every SQL
// read repeats current frozen-author authority and exact producer attempt/fence.
// No object IO occurs while a transaction is open.
type SQLMultiPreviewCatalogue struct {
	DB     *pgxpool.Pool
	Source MultiPreviewSource
	Bucket string
}

var _ AdoptedMultiCatalogueSource = SQLMultiPreviewCatalogue{}

func (s SQLMultiPreviewCatalogue) scope(who auth.Identity, t, p, task uuid.UUID, op string, root v2input.Ref, bucket string) bool {
	return s.DB != nil && validateMultiPreviewSource(s.Source) == nil && who == s.Source.Principal && t == s.Source.Tenant && p == s.Source.Project && task == s.Source.Producer && op == s.Source.Binding.Operation && root == s.Source.Binding.Result && bucket == s.Bucket && len(bucket) >= 3 && len(bucket) <= 63
}
func (s SQLMultiPreviewCatalogue) read(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || validateMultiPreviewSource(s.Source) != nil {
		return v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", s.Source.Tenant.String()); err != nil {
		return err
	}
	if err = fn(ctx, tx); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		return err
	}
	return tx.Commit(ctx)
}
func (s SQLMultiPreviewCatalogue) args() []any {
	p := s.Source
	return []any{p.Tenant, p.Producer, p.Binding.Operation, p.Attempt, p.Fence, p.Binding.Result.ArtifactID, p.Binding.Result.SHA256}
}
func (s SQLMultiPreviewCatalogue) LoadObject(ctx context.Context, who auth.Identity, t, p, task uuid.UUID, op string, root v2input.Ref, bucket string) (AdoptedMultiBinding, objectstore.Object, error) {
	if !s.scope(who, t, p, task, op, root, bucket) {
		return AdoptedMultiBinding{}, objectstore.Object{}, v2input.ErrInvalid
	}
	obj, err := s.locator(ctx, "SELECT * FROM photo.v29_multi_preview_read_object($1,$2,$3,$4,$5,$6,$7)", s.args(), root, "application/json", 1048576)
	if err != nil {
		return AdoptedMultiBinding{}, objectstore.Object{}, err
	}
	if !validAdoptedMultiRootObject(obj, root) {
		return AdoptedMultiBinding{}, objectstore.Object{}, v2input.ErrInvalid
	}
	return s.Source.Binding, obj, nil
}
func (s SQLMultiPreviewCatalogue) LoadChildObject(ctx context.Context, who auth.Identity, t, p, task uuid.UUID, op string, root, child v2input.Ref, path, contract, bucket string) (objectstore.Object, error) {
	media, limit, ok := multiChildObjectLimit(path, contract)
	if !ok || !validLayerArtifactRef(child) || child.ArtifactID == root.ArtifactID || !s.scope(who, t, p, task, op, root, bucket) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	args := append(s.args(), child.ArtifactID, child.SHA256, path, contract)
	return s.locator(ctx, "SELECT * FROM photo.v29_multi_preview_read_child($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", args, child, media, limit)
}
func (s SQLMultiPreviewCatalogue) locator(ctx context.Context, query string, args []any, ref v2input.Ref, media string, limit int64) (objectstore.Object, error) {
	var obj objectstore.Object
	var bucket string
	err := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, args...).Scan(&bucket, &obj.Key, &obj.VersionID, &obj.Bytes, &obj.ContentType)
	})
	if err != nil {
		return objectstore.Object{}, err
	}
	obj.SHA256 = ref.SHA256
	if bucket != s.Bucket || !validMultiChildObject(obj, ref, media, limit, false) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return obj, nil
}
func (s SQLMultiPreviewCatalogue) LoadOriginalInventory(ctx context.Context, who auth.Identity, t, p, task uuid.UUID, expected AdoptedMultiBinding, bucket string) (MultiOriginalInventory, error) {
	if expected != s.Source.Binding || !s.scope(who, t, p, task, expected.Operation, expected.Result, bucket) {
		return MultiOriginalInventory{}, v2input.ErrInvalid
	}
	var raw []byte
	err := s.read(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.v29_multi_preview_read_originals($1,$2,$3,$4,$5,$6,$7)", s.args()...).Scan(&raw)
	})
	if err != nil {
		return MultiOriginalInventory{}, err
	}
	if raw == nil {
		return MultiOriginalInventory{}, v2input.ErrExpired
	}
	return decodeMultiOriginalInventory(raw, expected, task, bucket)
}
