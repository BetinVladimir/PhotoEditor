package v2portrait

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// DraftTask describes a blocked producer. A descriptor is recovery data, not
// authority to read its inputs or admit it to a worker.
func (p RelightAnalysisPlan) DraftTask(ctx context.Context) (v2input.DraftTask, error) {
	var zero v2input.DraftTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	s := p.Snapshot()
	if s.Run == uuid.Nil || s.Workflow == uuid.Nil || s.Task == uuid.Nil || s.ParamsDocument == uuid.Nil || s.ReservedBytes <= 8388608 || len(s.Inputs) < 4 || len(s.Inputs) > 64 || !json.Valid(s.Profile) {
		return zero, v2input.ErrInvalid
	}
	var config struct {
		Base v2input.SessionVersionBinding `json:"base"`
	}
	if json.Unmarshal(s.Config, &config) != nil || len(s.SourceJCS) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	if _, err := VerifyDonorWorkingImage(config.Base, []byte(s.SourceJCS)); err != nil {
		return zero, err
	}
	if s.Operation != "depth.predict" && s.Operation != "mask.person" {
		return zero, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(s.Run)
	if err != nil || workflow != s.Workflow {
		return zero, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, s.Operation, 0)
	if err != nil || task != s.Task || uuid.NewSHA1(task, []byte("params")) != s.ParamsDocument {
		return zero, v2input.ErrInvalid
	}
	paramsSHA, err := canonical.Hash(json.RawMessage(s.Params))
	if err != nil || paramsSHA != s.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	foundParams := false
	for i, entry := range s.Inputs {
		if !validRelightTransportRef(entry.Ref) || i > 0 && s.Inputs[i-1].Ref.ArtifactID.String() >= entry.Ref.ArtifactID.String() {
			return zero, v2input.ErrInvalid
		}
		if entry.Ref.ArtifactID == s.ParamsDocument {
			if entry.Contract != "Params" || entry.Ref.SHA256 != s.ParamsSHA {
				return zero, v2input.ErrInvalid
			}
			foundParams = true
		}
	}
	if !foundParams {
		return zero, v2input.ErrInvalid
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{s.Task, s.Operation, "1", s.Inputs})
	if err != nil {
		return zero, err
	}
	key, err := v2input.NodeKey(s.Operation, 0)
	if err != nil {
		return zero, err
	}
	// Both model operations are installed on model-capable GPU-class workers,
	// including CPU and Metal backends; backend identity stays in the profile.
	return v2input.DraftTask{ID: s.Task, NodeKey: key, Operation: s.Operation, OperationVersion: "1", ResourceClass: "gpu", ParamsArtifactID: s.ParamsDocument, ParamsSHA: s.ParamsSHA, InputDigest: digest, ExecutionProfile: s.Profile}, nil
}

func (p RelightAnalysisPlan) Descriptor(ctx context.Context) ([]byte, error) {
	task, err := p.DraftTask(ctx)
	if err != nil {
		return nil, err
	}
	s := p.Snapshot()
	return canonical.Marshal(struct {
		SchemaVersion int               `json:"schema_version"`
		Task          v2input.DraftTask `json:"task"`
		Entries       []v2input.Entry   `json:"entries"`
		ReservedBytes int64             `json:"reserved_bytes"`
	}{1, task, s.Inputs, s.ReservedBytes})
}
