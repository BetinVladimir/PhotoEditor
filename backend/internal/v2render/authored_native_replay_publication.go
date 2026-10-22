package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// This dedicated publisher has exactly two purposes. It performs the same real
// immutable put/recover/version verification as artifactjson, with the separate
// protected 02617 repository boundaries (not a generic task_params substitute).
type AuthoredNativeReplayPublication struct {
	Scheduler *pgxpool.Pool
	Objects   AuthoredNativeReplayObjects
	Bucket    string
}

func nativeReplayIDs(request []byte) (uuid.UUID, uuid.UUID, uuid.UUID, v2input.Ref, error) {
	fail := func() (uuid.UUID, uuid.UUID, uuid.UUID, v2input.Ref, error) {
		return uuid.Nil, uuid.Nil, uuid.Nil, v2input.Ref{}, v2input.ErrInvalid
	}
	decoded, err := v2input.DecodeAuthoredNativeReplayRequest(request)
	if err != nil {
		return fail()
	}
	return decoded.IDs()
}
func (p AuthoredNativeReplayPublication) transaction(ctx context.Context, tenant uuid.UUID, fn func(pgx.Tx) error) error {
	if p.Scheduler == nil || tenant == uuid.Nil {
		return v2input.ErrInvalid
	}
	return database.TenantTransaction(ctx, p.Scheduler, tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return fn(tx)
	})
}
func (p AuthoredNativeReplayPublication) Publish(ctx context.Context, tenant uuid.UUID, request []byte, kind string, raw []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	request = append([]byte(nil), request...)
	raw = append([]byte(nil), raw...)
	if p.Objects == nil || p.Bucket == "" || (kind != "request" && kind != "evidence") || len(raw) < 2 || len(raw) > 4<<20 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	run, _, task, requestRef, err := nativeReplayIDs(request)
	if err != nil {
		return v2input.Ref{}, err
	}
	body, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, body) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	h := sha256.Sum256(body)
	digest := hex.EncodeToString(h[:])
	id := uuid.NewSHA1(run, []byte(kind))
	if kind == "request" && !bytes.Equal(body, request) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if kind == "evidence" {
		var e struct {
			Request v2input.Ref `json:"request"`
			SHA     string      `json:"request_sha256"`
		}
		if json.Unmarshal(body, &e) != nil || e.Request != requestRef || e.SHA != requestRef.SHA256 {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		if err := p.verifyEvidence(ctx, tenant, task, request, body); err != nil {
			return v2input.Ref{}, err
		}
	}
	var artifact, token uuid.UUID
	var ready bool
	err = p.transaction(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT * FROM photo.reserve_authored_native_replay_document($1,$2,$3,$4,$5)`, tenant, task, kind, string(body), p.Bucket).Scan(&artifact, &token, &ready)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return v2input.Ref{}, artifactjson.ErrStale
	}
	if err != nil {
		return v2input.Ref{}, err
	}
	if artifact != id || token == uuid.Nil {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	expected := v2input.Ref{ArtifactID: id, SHA256: digest}
	if ready {
		return expected, ctx.Err()
	}
	key, err := objectstore.DocumentKey(tenant, run, id)
	if err != nil {
		return v2input.Ref{}, err
	}
	object, err := p.Objects.PutImmutable(ctx, key, bytes.NewReader(body), int64(len(body)), digest, "application/json")
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		if ctx.Err() != nil {
			return v2input.Ref{}, ctx.Err()
		}
		object, err = p.Objects.RecoverImmutable(ctx, key, int64(len(body)), digest, "application/json")
	}
	if err != nil {
		return v2input.Ref{}, err
	}
	if object.Key != key || object.SHA256 != digest || object.Bytes != int64(len(body)) || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	var accepted bool
	err = p.transaction(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.complete_authored_native_replay_document($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, task, kind, string(body), id, token, p.Bucket, object.VersionID).Scan(&accepted)
	})
	if err != nil {
		return v2input.Ref{}, err
	}
	if !accepted {
		return v2input.Ref{}, artifactjson.ErrStale
	}
	return expected, ctx.Err()
}
