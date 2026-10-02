package v2input

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"sort"
)

type LensDraftConfig struct {
	ProjectID  uuid.UUID      `json:"project_id"`
	SnapshotID uuid.UUID      `json:"input_snapshot_id"`
	Lens       LensDraftNodes `json:"v25_lens"`
}
type LensDraftNodes struct {
	AuthSessionID uuid.UUID          `json:"auth_session_id"`
	Nodes         []LensDocumentPlan `json:"nodes"`
}
type CompiledLensDraft struct {
	Config      LensDraftConfig
	Body        json.RawMessage
	SHA256      string
	OutputBytes int64
}

// CompileLensDraftConfig builds the immutable authority/document plan consumed
// by SQL scope guards. It creates no run and grants no qualification or access.
func CompileLensDraftConfig(project, snapshot, session, run uuid.UUID, tasks []FrozenLensTask) (CompiledLensDraft, error) {
	var empty CompiledLensDraft
	if project == uuid.Nil || snapshot == uuid.Nil || session == uuid.Nil || run == uuid.Nil || len(tasks) < 1 || len(tasks) > 5000 {
		return empty, ErrInvalid
	}
	workflow, err := WorkflowID(run)
	if err != nil {
		return empty, err
	}
	config := LensDraftConfig{ProjectID: project, SnapshotID: snapshot, Lens: LensDraftNodes{AuthSessionID: session, Nodes: make([]LensDocumentPlan, 0, len(tasks))}}
	seen := map[uuid.UUID]bool{}
	var output int64
	for _, task := range tasks {
		if !validTaskIdentity(workflow, task.task, task.key) || seen[task.task] {
			return empty, ErrInvalid
		}
		seen[task.task] = true
		plan, err := task.DocumentPlan()
		if err != nil {
			return empty, err
		}
		var profile sessionDescribeExecutionProfile
		if json.Unmarshal(plan.Profile, &profile) != nil || profile.Backend != "cpu" || profile.RendererVersion != "v2.2" || profile.ResourceEstimate.OutputBytes <= 0 {
			return empty, ErrInvalid
		}
		if output > 9007199254740991-profile.ResourceEstimate.OutputBytes {
			return empty, ErrInvalid
		}
		output += profile.ResourceEstimate.OutputBytes
		config.Lens.Nodes = append(config.Lens.Nodes, plan)
	}
	sort.Slice(config.Lens.Nodes, func(i, j int) bool {
		return bytes.Compare(config.Lens.Nodes[i].TaskID[:], config.Lens.Nodes[j].TaskID[:]) < 0
	})
	body, err := canonical.Marshal(config)
	if err != nil || len(body) > 1048576 {
		return empty, ErrInvalid
	}
	sha, err := canonical.Hash(config)
	if err != nil {
		return empty, err
	}
	return CompiledLensDraft{config, body, sha, output}, nil
}
