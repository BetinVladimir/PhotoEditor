package provenance

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

// Frozen recovery evidence, not a workflow ticket. SQL pins the complete package
// on the first call and refuses replacement under the same actor/key/request.
type signingPendingDocument struct {
	QueueClass string            `json:"queue_class,omitempty"`
	Schema     int               `json:"schema_version"`
	Tenant     uuid.UUID         `json:"tenant_id"`
	Actor      uuid.UUID         `json:"actor_id"`
	Export     uuid.UUID         `json:"export_id"`
	Workflow   uuid.UUID         `json:"workflow_id"`
	Run        uuid.UUID         `json:"run_id"`
	TaskID     uuid.UUID         `json:"task_id"`
	RequestSHA string            `json:"request_sha"`
	Request    string            `json:"request_jcs"`
	Params     string            `json:"params_jcs"`
	Lineage    string            `json:"lineage_jcs"`
	Source     json.RawMessage   `json:"source_pins"`
	Bucket     string            `json:"bucket"`
	Task       v2input.DraftTask `json:"task"`
	Inputs     []v2input.Entry   `json:"inputs"`
}

func compileSigningPending(who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string) ([]byte, error) {
	i := prepared.intent
	if who.UserID != i.actor || who.SessionID != i.session || !idempotency.ValidKey(i.key) || ready.Tenant != i.tenant || ready.Project != i.project || ready.ExportID != i.export || ready.Object.ContentType != prepared.mime || i.workflow != uuid.NewSHA1(i.tenant, []byte("provenance:"+i.actor.String()+":"+i.key)) || i.run != uuid.NewSHA1(i.workflow, []byte("run")) || i.task != uuid.NewSHA1(i.workflow, []byte("provenance.sign:0")) || bodyDigest(i.request) != i.requestSHA {
		return nil, ErrInvalid
	}
	params, err := DecodeSignParams(i.params)
	if err != nil {
		return nil, err
	}
	if params.Export != ready.Ref {
		return nil, ErrInvalid
	}
	pins, err := signingLineageStoragePins(who, ready, params.Lineage, i.lineage, bucket)
	if err != nil {
		return nil, err
	}
	task := prepared.compiled.Task()
	doc, body := prepared.compiled.ParamsDocument()
	if task.ID != i.task || task.Operation != "provenance.sign" || task.OperationVersion != "1" || task.ResourceClass != "cpu" || task.ParamsArtifactID != doc.ArtifactID || task.ParamsSHA != doc.SHA256 || bodyDigest(body) != doc.SHA256 || string(body) != string(i.params) {
		return nil, ErrInvalid
	}
	inputs := prepared.compiled.Inputs()
	digest, err := canonical.Hash(inputs)
	if err != nil || digest != task.InputDigest {
		return nil, ErrInvalid
	}
	queue := "batch"
	if i.legacyQueueOmitted {
		queue = ""
	}
	raw, err := canonical.Marshal(signingPendingDocument{QueueClass: queue, Schema: 1, Tenant: i.tenant, Actor: i.actor, Export: i.export, Workflow: i.workflow, Run: i.run, TaskID: i.task, RequestSHA: i.requestSHA, Request: string(i.request), Params: string(i.params), Lineage: string(i.lineage), Source: pins, Bucket: bucket, Task: task, Inputs: inputs})
	if err != nil {
		return nil, err
	}
	if len(raw) > 10<<20 {
		return nil, ErrInvalid
	}
	return raw, nil
}

// PinSigningPending must run in an API transaction before domain locks. No
// external I/O, task creation or dispatch occurs here. Commit failure must not
// expose the outcome. Changed evidence is a conflict, never silently overwritten.
func PinSigningPending(ctx context.Context, tx SigningAdmissionTransaction, who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if tx == nil {
		return "", ErrInvalid
	}
	raw, err := compileSigningPending(who, ready, prepared, bucket)
	if err != nil {
		return "", err
	}
	i := prepared.intent
	var outcome string
	if err := tx.QueryRow(ctx, `SELECT outcome FROM photo.pin_v31_signing_pending($1,$2,$3,$4,$5,$6)`, i.tenant, who.UserID, who.SessionID, i.key, i.requestSHA, string(raw)).Scan(&outcome); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch outcome {
	case "created", "pending":
		return outcome, nil
	case "conflict":
		return "", artifactjson.ErrConflict
	case "stale":
		var unavailable bool
		if err := tx.QueryRow(ctx, `SELECT photo.v31_signing_adopter_unavailable($1,$2,$3,$4)`, i.tenant, who.UserID, who.SessionID, string(raw)).Scan(&unavailable); err != nil {
			return "", err
		}
		if unavailable {
			return "", domain.NewError("SIGNING_UNAVAILABLE", 503)
		}
		return "", artifactjson.ErrStale
	default:
		return "", artifactjson.ErrStale
	}
}
