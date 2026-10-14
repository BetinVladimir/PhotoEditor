package delivery

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// DelegatedSigningDraftDocuments are the canonical bytes to be checked and
// persisted by the dedicated scheduler draft admission. Params remain a
// separate immutable task_params publication after that draft is present.
type DelegatedSigningDraftDocuments struct {
	Config []byte
	Inputs []byte
	Params []byte
	ParamsRef v2input.Ref
}

func BuildDelegatedSigningDraftDocuments(ctx context.Context,
	scope SignerDelegationScope, intent provenance.FrozenSigningIntent,
	task provenance.FrozenSigningTask) (DelegatedSigningDraftDocuments, error) {
	var zero DelegatedSigningDraftDocuments
	if err := ctx.Err(); err != nil { return zero, err }
	planned := task.Task()
	ref, params := task.ParamsDocument()
	entries := task.Inputs()
	if scope.TenantID == uuid.Nil || scope.ProjectID == uuid.Nil ||
		scope.ExportID == uuid.Nil || scope.ActorID == uuid.Nil ||
		intent.WorkflowID() != scope.WorkflowID || intent.TaskID() != scope.TaskID ||
		intent.RunID() != uuid.NewSHA1(scope.WorkflowID, []byte("run")) ||
		intent.ProjectID() != scope.ProjectID || planned.ID != scope.TaskID ||
		planned.Operation != "provenance.sign" || planned.NodeKey != "provenance.sign:0" ||
		planned.ParamsArtifactID != ref.ArtifactID || planned.ParamsSHA != ref.SHA256 ||
		len(entries) != 3 || len(params) < 2 || len(params) > 65536 ||
		!shaPattern.MatchString(intent.RequestSHA()) ||
		digest(params) != ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	inputDigest, err := canonical.Hash(entries)
	if err != nil { return zero, err }
	if inputDigest != planned.InputDigest { return zero, v2input.ErrInvalid }
	inputs, err := canonical.Marshal(entries)
	if err != nil { return zero, err }
	config, err := canonical.Marshal(map[string]any{
		"schema_version": 1,
		"project_id": scope.ProjectID,
		"workflow_type": "provenance",
		"export_id": scope.ExportID,
		"request_sha": intent.RequestSHA(),
		"snapshot_id": uuid.NewSHA1(scope.WorkflowID,
			[]byte("provenance:artifact-snapshot:1")),
		"snapshot_digest": planned.InputDigest,
		"params": ref,
		"queue_class": "batch",
	})
	if err != nil { return zero, err }
	if len(inputs) < 2 || len(inputs) > 16384 ||
		len(config) < 2 || len(config) > 65536 { return zero, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil { return zero, err }
	return DelegatedSigningDraftDocuments{Config: config, Inputs: inputs,
		Params: append([]byte(nil), params...), ParamsRef: ref}, nil
}
