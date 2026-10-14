package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Request owns immutable user intent before catalog/object IO. RequestSHA binds
// only submitted content; runtime/model qualification is server-owned.
type Request struct {
	TenantID, ActorID, RunID, WorkflowID uuid.UUID
	Intent                               v2input.SemanticEditIntent
	Body                                 json.RawMessage
	RequestSHA                           string
}

func CompileRequest(tenant, actor uuid.UUID, key string, raw []byte) (Request, error) {
	var empty Request
	run, err := v2input.RunID(tenant, actor, key)
	if err != nil {
		return empty, err
	}
	intent, err := v2input.DecodeSemanticEditIntent(raw)
	if err != nil {
		return empty, err
	}
	body, err := canonical.Marshal(intent)
	if err != nil {
		return empty, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return empty, err
	}
	digest, err := canonical.Hash(struct {
		Tenant uuid.UUID                  `json:"tenant_id"`
		Actor  uuid.UUID                  `json:"actor_id"`
		Intent v2input.SemanticEditIntent `json:"intent"`
	}{tenant, actor, intent})
	if err != nil {
		return empty, err
	}
	return Request{tenant, actor, run, workflow, intent, body, digest}, nil
}

// BindTargets validates the exact pinned snapshot and preserves its ordinal
// order. Repository authorization of project/source/mask remains mandatory.
func BindTargets(request Request, ref v2input.Ref, raw json.RawMessage) ([]v2input.SessionVersionBinding, error) {
	body, err := canonical.Marshal(request.Intent)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	owned, err := v2input.DecodeSemanticEditIntent(body)
	if err != nil {
		return nil, err
	}
	snapshot, err := v2input.DecodePinnedSessionInputSnapshot(ref, raw)
	if err != nil || snapshot.SnapshotID != owned.TargetSnapshotID {
		return nil, v2input.ErrInvalid
	}
	targets := make([]v2input.SessionVersionBinding, len(snapshot.Items))
	for i, item := range snapshot.Items {
		targets[i] = item.Binding
	}
	return targets, nil
}
