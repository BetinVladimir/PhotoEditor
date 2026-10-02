package v2region

import (
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// DraftShell adapts a prepared node to the existing two-transaction admission
// protocol. Snapshot scope/membership, live authority and runtime qualification
// must be proved by the admission store before this shell can be persisted.
func (n FrozenRegionNode) DraftShell(who auth.Identity, key string, snapshot uuid.UUID, policySHA string, reservedBytes int64) (v2input.DraftShell, []v2input.TaskSource, error) {
	var empty v2input.DraftShell
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || snapshot == uuid.Nil || !validRegionSHA(policySHA) || reservedBytes < 1 || reservedBytes > 9007199254740991 || n.inputs.Open == nil {
		return empty, nil, ErrRegionInvalid
	}
	var node regionNodeDocument
	var params regionPlanDocument
	if json.Unmarshal(n.body, &node) != nil || json.Unmarshal([]byte(node.ParamsJCS), &params) != nil {
		return empty, nil, ErrRegionInvalid
	}
	run, err := v2input.RunID(params.TenantID, who.UserID, key)
	if err != nil {
		return empty, nil, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return empty, nil, err
	}
	task, err := v2input.TaskID(workflow, "region.preview", 0)
	if err != nil || task != node.TaskID || node.InputDigest != n.inputs.InputDigest {
		return empty, nil, ErrRegionInvalid
	}
	region, err := n.ConfigJSON(who.SessionID)
	if err != nil {
		return empty, nil, err
	}
	config, err := canonical.Marshal(struct {
		Project  uuid.UUID       `json:"project_id"`
		Snapshot uuid.UUID       `json:"input_snapshot_id"`
		Region   json.RawMessage `json:"v26_region"`
	}{params.ProjectID, snapshot, region})
	if err != nil {
		return empty, nil, err
	}
	configSHA, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return empty, nil, err
	}
	nodeKey, _ := v2input.NodeKey("region.preview", 0)
	shell := v2input.DraftShell{Tenant: params.TenantID, Run: run, WorkflowID: workflow, Project: params.ProjectID, Snapshot: snapshot, Actor: who.UserID, Workflow: "preview", PolicySHA: policySHA, ConfigSHA: configSHA, Config: config, Bytes: reservedBytes,
		Tasks: []v2input.DraftTask{{ID: task, NodeKey: nodeKey, Operation: "region.preview", OperationVersion: "1", ResourceClass: "gpu", ParamsArtifactID: node.Params.ArtifactID, ParamsSHA: node.Params.SHA256, InputDigest: node.InputDigest, ExecutionProfile: append(json.RawMessage(nil), node.Profile...)}}, Edges: []v2input.DraftEdge{}}
	inputs := n.Inputs()
	return shell, []v2input.TaskSource{{ID: task, Open: inputs.Open, Direct: inputs.Direct}}, nil
}
