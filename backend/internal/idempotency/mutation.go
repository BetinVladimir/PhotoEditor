package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"time"
)

type Scope struct {
	Tenant, Principal         uuid.UUID
	PrincipalType, Route, Key string
}
type Response struct {
	Status     int
	Body       json.RawMessage
	ResourceID *uuid.UUID
	Replayed   bool
}

func ValidKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

// Use must be called before domain row locks and within the same transaction as fn.
func Use(ctx context.Context, tx pgx.Tx, scope Scope, request any, expires time.Time, fn func() (Response, error)) (Response, error) {
	if !ValidKey(scope.Key) {
		return Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	hash, err := canonical.Hash(request)
	if err != nil {
		return Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	_, err = tx.Exec(ctx, `INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING`, scope.Tenant, scope.PrincipalType, scope.Principal, scope.Route, scope.Key, hash, expires)
	if err != nil {
		return Response{}, err
	}
	var id uuid.UUID
	var storedHash string
	var status *int
	var body []byte
	var resource *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id,request_hash,response_status,response_body,resource_id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type=$2 AND principal_id=$3 AND route=$4 AND key=$5 FOR UPDATE`, scope.Tenant, scope.PrincipalType, scope.Principal, scope.Route, scope.Key).Scan(&id, &storedHash, &status, &body, &resource)
	if err != nil {
		return Response{}, err
	}
	if hash != storedHash {
		return Response{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	}
	if status != nil {
		b, e := canonical.Bytes(body)
		return Response{Status: *status, Body: b, ResourceID: resource, Replayed: true}, e
	}
	out, err := fn()
	if err != nil {
		return Response{}, err
	}
	if out.Status < 200 || out.Status > 299 {
		return Response{}, errors.New("idempotent mutation requires successful response")
	}
	out.Body, err = canonical.Bytes(out.Body)
	if err != nil {
		return Response{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE photo.idempotency_records SET response_status=$3,response_body=$4,resource_id=$5 WHERE tenant_id=$1 AND id=$2`, scope.Tenant, id, out.Status, out.Body, out.ResourceID)
	return out, err
}
