package v2render

import (
 "bytes"
 "encoding/json"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"time"
)

// PerspectivePreviewSource is a live read observation, not an admission lease.
// Every publication/seal/adoption must repeat these identities under SQL locks.
type PerspectivePreviewSource struct {
 Replacement PerspectivePreviewReplacementIdentity
	Tenant, Project, Run, Workflow, Producer, ResultID uuid.UUID
	Result                                             v2input.Ref
	Attempt                                            int
	Fence                                              int64
	Principal                                          auth.Identity
	Base                                               v2input.SessionVersionBinding
}

type PerspectivePreviewReplacementIdentity struct {
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 OriginalTask uuid.UUID `json:"original_task_id"`
 OriginalRun uuid.UUID `json:"original_run_id"`
}

func decodePerspectivePreviewReplacementIdentity(raw []byte)(PerspectivePreviewReplacementIdentity,error){
 if len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return PerspectivePreviewReplacementIdentity{},nil}
 if len(raw)>1024{return PerspectivePreviewReplacementIdentity{},v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil{return PerspectivePreviewReplacementIdentity{},v2input.ErrInvalid}
 var value PerspectivePreviewReplacementIdentity
 if json.Unmarshal(normal,&value)!=nil{return PerspectivePreviewReplacementIdentity{},v2input.ErrInvalid}
 exact,err:=canonical.Marshal(value);if err!=nil||!bytes.Equal(normal,exact){return PerspectivePreviewReplacementIdentity{},v2input.ErrInvalid}
 if value.Review==uuid.Nil||value.OriginalTask==uuid.Nil||value.OriginalRun==uuid.Nil{return PerspectivePreviewReplacementIdentity{},v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(value.DecisionHash);err!=nil{return PerspectivePreviewReplacementIdentity{},v2input.ErrInvalid}
 return value,nil
}

type SQLPerspectivePreviewSource struct{ DB *pgxpool.Pool }

func (s SQLPerspectivePreviewSource) Resolve(ctx context.Context, tenant, producer uuid.UUID) (PerspectivePreviewSource, error) {
	var zero PerspectivePreviewSource
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || tenant == uuid.Nil || producer == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	value := PerspectivePreviewSource{Tenant: tenant, Producer: producer}
	err = tx.QueryRow(ctx, "SELECT * FROM photo.v29_perspective_preview_source($1,$2)", tenant, producer).Scan(
		&value.Project, &value.Run, &value.Workflow, &value.ResultID, &value.Result.ArtifactID, &value.Result.SHA256,
		&value.Attempt, &value.Fence, &value.Principal.UserID, &value.Principal.SessionID,
		&value.Base.AssetVersionID, &value.Base.Source.ArtifactID, &value.Base.Source.SHA256, &value.Base.CanvasHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
 var replacementRaw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_perspective_preview_replacement_identity($1,$2)",tenant,producer).Scan(&replacementRaw);err!=nil{return zero,err}
 value.Replacement,err=decodePerspectivePreviewReplacementIdentity(replacementRaw);if err!=nil{return zero,err}
	if err = validatePerspectivePreviewSource(value); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return value, nil
}

// ValidatePerspectivePreviewSource checks complete ordinary or replacement identity.
// It validates structure only; callers must retain current SQL/object authority.
func ValidatePerspectivePreviewSource(value PerspectivePreviewSource) error {
 return validatePerspectivePreviewSource(value)
}

func validatePerspectivePreviewSource(value PerspectivePreviewSource) error {
	if value.Tenant == uuid.Nil || value.Project == uuid.Nil || value.Run == uuid.Nil || value.Workflow == uuid.Nil || value.Producer == uuid.Nil || value.ResultID == uuid.Nil || value.Principal.UserID == uuid.Nil || value.Principal.SessionID == uuid.Nil || value.Attempt < 1 || value.Fence < 1 || value.Base.AssetVersionID == uuid.Nil || !validLayerArtifactRef(value.Result) || !validLayerArtifactRef(value.Base.Source) || !validLayerArtifactRef(v2input.Ref{ArtifactID: value.Base.AssetVersionID, SHA256: value.Base.CanvasHash}) || value.Result.ArtifactID == value.Base.Source.ArtifactID {
		return v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(value.Run)
	if err != nil || workflow != value.Workflow {
		return v2input.ErrInvalid
	}
 if value.Replacement!=(PerspectivePreviewReplacementIdentity{}){
  identity:=value.Replacement
  if identity.Review==uuid.Nil||identity.OriginalTask==uuid.Nil||identity.OriginalRun==uuid.Nil{return v2input.ErrInvalid}
  if _,err:=domain.ParseDigest(identity.DecisionHash);err!=nil{return v2input.ErrInvalid}
  namespace:=uuid.NewSHA1(value.Tenant,[]byte("V29.NegativeReplacement/v2:"+value.Project.String()+":"+identity.Review.String()+":"+identity.DecisionHash))
  run:=uuid.NewSHA1(namespace,[]byte("run:"+identity.OriginalRun.String()+":"+identity.OriginalTask.String()))
  task:=uuid.NewSHA1(namespace,[]byte("task:"+identity.OriginalTask.String()))
  if value.Run!=run||value.Producer!=task{return v2input.ErrInvalid}
  return nil
 }
	producer, err := v2input.TaskID(workflow, "optical.perspective", 0)
	if err != nil || producer != value.Producer {
		return v2input.ErrInvalid
	}
	return nil
}
