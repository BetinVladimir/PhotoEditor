package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"time"
)

// MultiPreviewSource is a scheduler observation, never a cached grant or lease.
// Current producer/original/actor scope must be repeated at preview admission.
type MultiPreviewSource struct {
	Tenant, Project, Producer uuid.UUID
	Binding                   AdoptedMultiBinding
	Attempt                   int
	Fence                     int64
	Principal                 auth.Identity
}

func validateMultiPreviewSource(v MultiPreviewSource) error {
	if v.Tenant == uuid.Nil || v.Project == uuid.Nil || v.Principal.UserID == uuid.Nil || v.Principal.SessionID == uuid.Nil || v.Attempt < 1 || v.Attempt > 6 || v.Fence < 1 {
		return v2input.ErrInvalid
	}
	if v.Binding.Replacement!=(MultiReplacementBindingIdentity{})&&(v.Binding.Replacement.Tenant!=v.Tenant||v.Binding.Replacement.Project!=v.Project){return v2input.ErrInvalid}
	return validateAdoptedMultiBinding(v.Binding, v.Producer, v.Binding.Operation, v.Binding.Result)
}

// Preview identity includes producer fencing, immutable result and display ICC.
// A new attempt/fence or color profile can never silently reuse old media.
func MultiPreviewRunID(v MultiPreviewSource, srgb string) (uuid.UUID, error) {
	if err := validateMultiPreviewSource(v); err != nil {
		return uuid.Nil, err
	}
	if _, err := domain.ParseDigest(srgb); err != nil {
		return uuid.Nil, v2input.ErrInvalid
	}
	name := "V29.MultiPreview:" + v.Binding.Operation + ":" + v.Producer.String() + ":" + v.Binding.ResultID.String() + ":" + v.Binding.Result.SHA256 + ":" + strconv.Itoa(v.Attempt) + ":" + strconv.FormatInt(v.Fence, 10) + ":" + srgb
	return uuid.NewSHA1(v.Binding.Run, []byte(name)), nil
}

type SQLMultiPreviewSource struct{ DB *pgxpool.Pool }

func (s SQLMultiPreviewSource) Resolve(ctx context.Context, tenant, producer uuid.UUID, operation string) (MultiPreviewSource, error) {
	var empty MultiPreviewSource
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.DB == nil || tenant == uuid.Nil || producer == uuid.Nil || (operation != "optical.panorama" && operation != "optical.focus_stack") {
		return empty, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return empty, err
	}
	if role != "photo_scheduler" {
		return empty, v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return empty, err
	}
	v := MultiPreviewSource{Tenant: tenant, Producer: producer}
	v.Binding.Operation = operation
	err = tx.QueryRow(ctx, "SELECT * FROM photo.v29_multi_preview_source($1,$2,$3)", tenant, producer, operation).Scan(&v.Project, &v.Binding.Run, &v.Binding.Workflow, &v.Binding.ResultID, &v.Binding.Result.ArtifactID, &v.Binding.Result.SHA256, &v.Binding.Snapshot, &v.Binding.ParamsJCS, &v.Attempt, &v.Fence, &v.Principal.UserID, &v.Principal.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, v2input.ErrExpired
	}
	if err != nil {
		return empty, err
	}
 var identityRaw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_multi_replacement_identity($1,$2,$3,$4,$5)",tenant,v.Project,v.Principal.UserID,v.Principal.SessionID,producer).Scan(&identityRaw);err!=nil{return empty,err}
 v.Binding.Replacement,err=decodeMultiReplacementBindingIdentity(identityRaw);if err!=nil{return empty,err}
	if err := validateMultiPreviewSource(v); err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return v, nil
}
