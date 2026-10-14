package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Descriptor reconstructs a blocked task from owned originals. Its bytes are
// recovery data, not authorization to stage, publish transport or seal a graph.
func (p PreparedPerspective) Descriptor(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := p.draft.Snapshot()
	if err != nil {
		return nil, err
	}
	if snapshot.Run == uuid.Nil || snapshot.Task == uuid.Nil || snapshot.ParamsDocument == uuid.Nil || p.params.ID != snapshot.ParamsDocument || p.params.SHA256 != snapshot.ParamsSHA {
		return nil, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(snapshot.Run)
	if err != nil || workflow != snapshot.Workflow {
		return nil, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, "optical.perspective", 0)
	if err != nil || task != snapshot.Task || uuid.NewSHA1(task, []byte("params")) != snapshot.ParamsDocument {
		return nil, v2input.ErrInvalid
	}
	params, err := ParsePerspectiveParams([]byte(snapshot.ParamsJCS))
	if err != nil {
		return nil, err
	}
	qualified, err := p.qualification.Qualification()
	if err != nil {
		return nil, err
	}
	var profile struct {
		Resources PerspectiveResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(snapshot.Profile, &profile) != nil {
		return nil, v2input.ErrInvalid
	}
	plan, err := CompilePerspectiveTaskPlan(ctx, params.Base, []byte(snapshot.ParamsJCS), []byte(snapshot.SourceJCS), qualified, snapshot.MaxPixels, profile.Resources, snapshot.lineOriginals()...)
	if err != nil {
		return nil, err
	}
	if string(plan.Profile) != string(snapshot.Profile) || plan.ReservedBytes != snapshot.ReservedBytes || plan.ParamsSHA != snapshot.ParamsSHA {
		return nil, v2input.ErrInvalid
	}
	entries, _, err := PerspectiveTaskInputEntries(ctx, plan, []byte(snapshot.SourceJCS), qualified, v2input.Ref{ArtifactID: p.params.ID, SHA256: p.params.SHA256})
	if err != nil {
		return nil, err
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, "optical.perspective", "1", entries})
	if err != nil {
		return nil, err
	}
	node, err := v2input.NodeKey("optical.perspective", 0)
	if err != nil {
		return nil, err
	}
	draft := v2input.DraftTask{ID: task, NodeKey: node, Operation: "optical.perspective", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: p.params.ID, ParamsSHA: p.params.SHA256, InputDigest: digest, ExecutionProfile: plan.Profile}
	return canonical.Marshal(struct {
		SchemaVersion int               `json:"schema_version"`
		Task          v2input.DraftTask `json:"task"`
		Entries       []v2input.Entry   `json:"entries"`
		ReservedBytes int64             `json:"reserved_bytes"`
	}{1, draft, entries, snapshot.ReservedBytes})
}
