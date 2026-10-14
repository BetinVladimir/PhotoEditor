package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Original graph locator, not producer qualification or physical content proof.
type nativeInputDefectSavedLocator struct {
	params                                         v2input.Ref
	landmarksRun, landmarksTask, skinRun, skinTask uuid.UUID
	configSHA                                      string
	person                                         *nativeDefectPersonRecovery
}

func decodeNativeInputDefectSavedLocator(raw []byte, sha string, project uuid.UUID, who auth.Identity, request Request, intent, snapshot v2input.Ref) (nativeInputDefectSavedLocator, error) {
	var zero nativeInputDefectSavedLocator
	if len(raw) < 2 || len(raw) > 4<<20 || who.UserID != request.ActorID || who.SessionID == uuid.Nil || project == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	actual, err := canonical.Hash(json.RawMessage(raw))
	if err != nil || actual != sha {
		return zero, v2input.ErrConflict
	}
	var config struct {
		Project uuid.UUID `json:"project_id"`
		Native  struct {
			Schema         int                           `json:"schema_version"`
			Operation      string                        `json:"operation"`
			Parent         uuid.UUID                     `json:"parent_run_id"`
			RequestSHA     string                        `json:"request_sha"`
			Session        uuid.UUID                     `json:"auth_session_id"`
			Source         v2input.SessionVersionBinding `json:"source"`
			Intent         v2input.Ref                   `json:"intent"`
			Snapshot       v2input.Ref                   `json:"snapshot"`
			ParamsDocument uuid.UUID                     `json:"params_document"`
			Params         v2input.Ref                   `json:"params"`
			Task           uuid.UUID                     `json:"task_id"`
			LandmarksRun   uuid.UUID                     `json:"landmarks_run_id"`
			LandmarksTask  uuid.UUID                     `json:"landmarks_task_id"`
			SkinRun        uuid.UUID                     `json:"skin_run_id"`
			SkinTask       uuid.UUID                     `json:"skin_task_id"`
			Person         json.RawMessage               `json:"person_input"`
		} `json:"v27_defect_native_prepare"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return zero, v2input.ErrInvalid
	}
	n := config.Native
	_, workflow, err := defectPreparationIDs(request.RunID)
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, "defects.propose", 0)
	if err != nil {
		return zero, err
	}
	landmarksRun, landmarksWorkflow, err := nativeFacePreparationIDs(request.RunID, "face.landmarks")
	if err != nil {
		return zero, err
	}
	landmarksTask, err := v2input.TaskID(landmarksWorkflow, "face.landmarks", 0)
	if err != nil {
		return zero, err
	}
	skinRun := uuid.NewSHA1(request.RunID, []byte("semantic.mask.face_skin:reference"))
	skinWorkflow, err := v2input.WorkflowID(skinRun)
	if err != nil {
		return zero, err
	}
	skinTask, err := v2input.TaskID(skinWorkflow, "mask.face_skin", 0)
	if err != nil {
		return zero, err
	}
	if config.Project != project || n.Schema != 1 || n.Operation != "defects.propose" || n.Parent != request.RunID || n.RequestSHA != request.RequestSHA || n.Session != who.SessionID || n.Source != request.Intent.Reference || n.Intent != intent || n.Snapshot != snapshot || n.Task != task || n.ParamsDocument != uuid.NewSHA1(request.RunID, []byte("semantic.defect.params:reference")) || n.Params.ArtifactID == uuid.Nil || n.LandmarksRun != landmarksRun || n.LandmarksTask != landmarksTask || n.SkinRun != skinRun || n.SkinTask != skinTask {
		return zero, v2input.ErrConflict
	}
	for _, digest := range []string{n.Params.SHA256, intent.SHA256, snapshot.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	var person *nativeDefectPersonRecovery
	if len(n.Person) != 0 {
		m, err := decodeNativeDefectPersonRecovery(n.Person)
		if err != nil {
			return zero, err
		}
		person = &m
	}
	return nativeInputDefectSavedLocator{params: n.Params, landmarksRun: landmarksRun, landmarksTask: landmarksTask, skinRun: skinRun, skinTask: skinTask, configSHA: sha, person: person}, nil
}

func (s SQLTransferSourceReader) readNativeInputDefectSavedLocator(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref) (nativeInputDefectSavedLocator, error) {
	var zero nativeInputDefectSavedLocator
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || who.UserID != request.ActorID {
		return zero, v2input.ErrInvalid
	}
	run, _, err := defectPreparationIDs(request.RunID)
	if err != nil {
		return zero, err
	}
	var raw []byte
	var sha string
	err = tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT config_snapshot,config_hash FROM photo.semantic_native_input_defect_saved_config($1,$2,$3,$4,$5)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, run).Scan(&raw, &sha)
	})
	if err != nil {
		return zero, err
	}
	return decodeNativeInputDefectSavedLocator(raw, sha, s.ProjectID, who, request, intent, snapshot)
}
