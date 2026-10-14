package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

// ParamsJCS is the immutable ordered producer request, not a selected UI base.
// This comparable value owns all data and may be compared after object IO.
type AdoptedMultiBinding struct {
 Replacement MultiReplacementBindingIdentity `json:"-"`
	ResultID  uuid.UUID   `json:"result_id"`
	Result    v2input.Ref `json:"result"`
	Run       uuid.UUID   `json:"run_id"`
	Workflow  uuid.UUID   `json:"workflow_id"`
	Snapshot  uuid.UUID   `json:"snapshot_id"`
	Operation string      `json:"operation"`
	ParamsJCS string      `json:"params_jcs"`
}

func (b AdoptedMultiBinding) Inputs() ([]v2input.SessionVersionBinding, int, v2input.Ref, error) {
	switch b.Operation {
	case "optical.panorama":
		p, err := v2optical.ParsePanoramaParams([]byte(b.ParamsJCS))
		if err != nil {
			return nil, 0, v2input.Ref{}, err
		}
		return p.Frames, p.ReferenceOrdinal, p.Calibration, nil
	case "optical.focus_stack":
		p, err := v2optical.ParseFocusStackParams([]byte(b.ParamsJCS))
		if err != nil {
			return nil, 0, v2input.Ref{}, err
		}
		return p.Frames, p.ReferenceOrdinal, p.Calibration, nil
	default:
		return nil, 0, v2input.Ref{}, v2input.ErrInvalid
	}
}
func validateAdoptedMultiBinding(b AdoptedMultiBinding, producer uuid.UUID, operation string, expected v2input.Ref) error {
	if b.ResultID == uuid.Nil || b.Run == uuid.Nil || b.Workflow == uuid.Nil || b.Snapshot == uuid.Nil || b.Operation != operation || b.Result != expected || !validLayerArtifactRef(expected) {
		return v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(b.Run)
	if err != nil || workflow != b.Workflow {
		return v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, operation, 0)
	if b.Replacement!=(MultiReplacementBindingIdentity{}) {
  if err:=b.Replacement.validate(b.Run,producer);err!=nil{return err}
 } else if err != nil || task != producer {
		return v2input.ErrInvalid
	}
	frames, _, calibration, err := b.Inputs()
	if err != nil {
		return err
	}
	if calibration.ArtifactID == b.Result.ArtifactID {
		return v2input.ErrInvalid
	}
	for _, frame := range frames {
		if frame.Source.ArtifactID == b.Result.ArtifactID {
			return v2input.ErrInvalid
		}
	}
	return nil
}

type SQLAdoptedMultiSource struct{ DB *pgxpool.Pool }

// Binding proves the adopted root and every current original source in a short
// photo_api tenant transaction. It grants neither child IO nor Apply.
func (s SQLAdoptedMultiSource) Binding(ctx context.Context, who auth.Identity, tenantID, project, producer uuid.UUID, operation string) (AdoptedMultiBinding, error) {
	var b AdoptedMultiBinding
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil || project == uuid.Nil || producer == uuid.Nil || (operation != "optical.panorama" && operation != "optical.focus_stack") {
		return b, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return b, err
	}
	b.Operation = operation
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		err := tx.QueryRow(ctx, `SELECT result_id,artifact_id,sha256,run_id,workflow_id,snapshot_id,params_jcs
   FROM photo.v29_adopted_multi_binding($1,$2,$3,$4,$5,$6)`, tenantID, project, producer, who.UserID, who.SessionID, operation).Scan(&b.ResultID, &b.Result.ArtifactID, &b.Result.SHA256, &b.Run, &b.Workflow, &b.Snapshot, &b.ParamsJCS)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		if err != nil {
			return err
		}
  var identityRaw []byte
  if err=tx.QueryRow(ctx,"SELECT photo.read_v29_multi_replacement_identity($1,$2,$3,$4,$5)",tenantID,project,who.UserID,who.SessionID,producer).Scan(&identityRaw);err!=nil{return err}
  b.Replacement,err=decodeMultiReplacementBindingIdentity(identityRaw);if err!=nil{return err}
  if b.Replacement!=(MultiReplacementBindingIdentity{})&&(b.Replacement.Tenant!=tenantID||b.Replacement.Project!=project){return v2input.ErrInvalid}
		return validateAdoptedMultiBinding(b, producer, operation, b.Result)
	})
	if err != nil {
		return AdoptedMultiBinding{}, err
	}
	return b, nil
}
func (s SQLAdoptedMultiSource) LoadObject(ctx context.Context, who auth.Identity, tenantID, project, producer uuid.UUID, operation string, expected v2input.Ref, bucket string) (AdoptedMultiBinding, objectstore.Object, error) {
	var empty objectstore.Object
	if bucket == "" || !validLayerArtifactRef(expected) {
		return AdoptedMultiBinding{}, empty, v2input.ErrInvalid
	}
	b, err := s.Binding(ctx, who, tenantID, project, producer, operation)
	if err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	if err = validateAdoptedMultiBinding(b, producer, operation, expected); err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	var obj objectstore.Object
	var actualBucket string
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		err := tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes,media_type
   FROM photo.v29_adopted_multi_object($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, project, producer, who.UserID, who.SessionID, operation, expected.ArtifactID, expected.SHA256).Scan(&actualBucket, &obj.Key, &obj.VersionID, &obj.Bytes, &obj.ContentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		return err
	})
	if err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	obj.SHA256 = expected.SHA256
	if actualBucket != bucket || !validAdoptedMultiRootObject(obj, expected) {
		return AdoptedMultiBinding{}, empty, v2input.ErrInvalid
	}
	return b, obj, nil
}
func validAdoptedMultiRootObject(obj objectstore.Object, expected v2input.Ref) bool {
	return obj.Key != "" && obj.VersionID != "" && obj.VersionID != "null" && obj.SHA256 == expected.SHA256 && obj.ContentType == "application/json" && obj.Bytes >= 2 && obj.Bytes <= 1048576
}
