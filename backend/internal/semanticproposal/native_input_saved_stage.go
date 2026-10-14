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

type nativeInputSavedStage struct {
	run, task                               uuid.UUID
	params                                  v2input.Ref
	config, configSHA, inputDigest, profile string
	recovery                                nativePlanRecoveryMetadata
	person                                  *nativeDefectPersonRecovery
}

func nativeInputSavedStageIdentity(parent uuid.UUID, operation string) (uuid.UUID, uuid.UUID, string, error) {
	var run, workflow uuid.UUID
	var marker string
	var err error
	if parent == uuid.Nil {
		return uuid.Nil, uuid.Nil, "", v2input.ErrInvalid
	}
	switch operation {
	case "mask.person":
		run, workflow, err = personPreparationIDs(parent)
		marker = "v27_person_prepare"
	case "face.detect":
		run, workflow, err = detectorPreparationIDs(parent)
		marker = "v27_detector_prepare"
	case "face.landmarks", "face.parse":
		run, workflow, err = nativeFacePreparationIDs(parent, operation)
		marker = "v27_face_prepare"
	case "mask.face_skin":
		run = uuid.NewSHA1(parent, []byte("semantic.mask.face_skin:reference"))
		workflow, err = v2input.WorkflowID(run)
		marker = "v27_skin_prepare"
	case "defects.propose":
		run, workflow, err = defectPreparationIDs(parent)
		marker = "v27_defect_native_prepare"
	default:
		return uuid.Nil, uuid.Nil, "", v2input.ErrInvalid
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, "", err
	}
	task, err := v2input.TaskID(workflow, operation, 0)
	return run, task, marker, err
}

func decodeNativeInputSavedStage(raw []byte, sha string, task uuid.UUID, params v2input.Ref, inputDigest string, profile []byte, project uuid.UUID, who auth.Identity, request Request, intent, snapshot v2input.Ref, operation string) (nativeInputSavedStage, error) {
	var zero nativeInputSavedStage
	if len(raw) < 2 || len(raw) > 4<<20 || len(profile) < 2 || len(profile) > 1<<20 || project == uuid.Nil || who.SessionID == uuid.Nil || who.UserID != request.ActorID {
		return zero, v2input.ErrInvalid
	}
	run, expectedTask, marker, err := nativeInputSavedStageIdentity(request.RunID, operation)
	if err != nil {
		return zero, err
	}
	actual, err := canonical.Hash(json.RawMessage(raw))
	if err != nil || actual != sha || task != expectedTask {
		return zero, v2input.ErrConflict
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return zero, v2input.ErrInvalid
	}
	var actualProject uuid.UUID
	if json.Unmarshal(envelope["project_id"], &actualProject) != nil || actualProject != project {
		return zero, v2input.ErrConflict
	}
	var stage struct {
		Schema      int                           `json:"schema_version"`
		Operation   string                        `json:"operation"`
		Parent      uuid.UUID                     `json:"parent_run_id"`
		RequestSHA  string                        `json:"request_sha"`
		Session     uuid.UUID                     `json:"auth_session_id"`
		Source      v2input.SessionVersionBinding `json:"source"`
		Intent      v2input.Ref                   `json:"intent"`
		Snapshot    v2input.Ref                   `json:"snapshot"`
		Document    uuid.UUID                     `json:"params_document"`
		Params      v2input.Ref                   `json:"params"`
		Task        uuid.UUID                     `json:"task_id"`
		InputDigest string                        `json:"input_digest"`
		Recovery    json.RawMessage               `json:"recovery"`
		Person      json.RawMessage               `json:"person_input"`
	}
	if json.Unmarshal(envelope[marker], &stage) != nil {
		return zero, v2input.ErrInvalid
	}
	documentOperation := operation
	if operation == "defects.propose" {
		documentOperation = "defect"
	}
	document := uuid.NewSHA1(request.RunID, []byte("semantic."+documentOperation+".params:reference"))
	if stage.Schema != 1 || stage.Parent != request.RunID || stage.RequestSHA != request.RequestSHA || stage.Session != who.SessionID || stage.Source != request.Intent.Reference || stage.Intent != intent || stage.Snapshot != snapshot || stage.Document != document || stage.Task != task || stage.Params != params || stage.InputDigest != inputDigest || (operation != "face.detect" && operation != "mask.person" && stage.Operation != operation) || ((operation == "face.detect" || operation == "mask.person") && stage.Operation != "") || params.ArtifactID == uuid.Nil || intent.ArtifactID == uuid.Nil || snapshot.ArtifactID == uuid.Nil {
		return zero, v2input.ErrConflict
	}
	for _, digest := range []string{sha, inputDigest, params.SHA256, intent.SHA256, snapshot.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	recovery, err := decodeNativePlanRecoveryMetadata(stage.Recovery, v2input.DraftTask{Operation: operation, ExecutionProfile: profile})
	if err != nil {
		return zero, err
	}
	if recovery.RequestKey != "" {
		parent, err := v2input.RunID(request.TenantID, request.ActorID, recovery.RequestKey)
		if err != nil || parent != request.RunID {
			return zero, v2input.ErrConflict
		}
	}
	normalized, err := canonical.Bytes(profile)
	if err != nil {
		return zero, err
	}
	var person *nativeDefectPersonRecovery
	if len(stage.Person) != 0 {
		if operation != "defects.propose" {
			return zero, v2input.ErrInvalid
		}
		m, err := decodeNativeDefectPersonRecovery(stage.Person)
		if err != nil {
			return zero, err
		}
		person = &m
	}
	return nativeInputSavedStage{run: run, task: task, params: params, config: string(raw), configSHA: sha, inputDigest: inputDigest, profile: string(normalized), recovery: recovery, person: person}, nil
}

func (s SQLTransferSourceReader) readNativeInputSavedStage(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref, operation string) (nativeInputSavedStage, error) {
	var zero nativeInputSavedStage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	run, _, _, err := nativeInputSavedStageIdentity(request.RunID, operation)
	if err != nil {
		return zero, err
	}
	var raw, profile []byte
	var sha, inputDigest string
	var task uuid.UUID
	var params v2input.Ref
	err = tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT config_snapshot,config_hash,task_id,params_artifact_id,params_sha,input_digest,execution_profile FROM photo.semantic_native_input_saved_stage($1,$2,$3,$4,$5,$6,$7)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, run, operation).Scan(&raw, &sha, &task, &params.ArtifactID, &params.SHA256, &inputDigest, &profile)
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return decodeNativeInputSavedStage(raw, sha, task, params, inputDigest, profile, s.ProjectID, who, request, intent, snapshot, operation)
}
