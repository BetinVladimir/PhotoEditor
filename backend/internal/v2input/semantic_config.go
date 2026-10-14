package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type SemanticProtectionPin struct {
	Ordinal          int
	Params           Ref
	SourceProtection Ref
	TargetProtection Ref
}

type SemanticFrozenNode struct {
	TaskID           uuid.UUID `json:"task_id"`
	ParamsArtifactID uuid.UUID `json:"params_artifact_id"`
	ParamsSHA        string    `json:"params_sha"`
	SourceProtection Ref       `json:"source_protection"`
	TargetProtection Ref       `json:"target_protection"`
}

type SemanticFrozenNodes struct {
	Qualification  *SemanticQualificationPin `json:"qualification,omitempty"`
	Snapshot       *Ref                      `json:"snapshot,omitempty"`
	SnapshotDigest string                    `json:"snapshot_digest,omitempty"`
	AuthSessionID  uuid.UUID                 `json:"auth_session_id"`
	Nodes          []SemanticFrozenNode      `json:"nodes"`
}

type SemanticDraftConfig struct {
 QueueClass string `json:"queue_class,omitempty"`
	ProjectID  uuid.UUID           `json:"project_id"`
	SnapshotID uuid.UUID           `json:"input_snapshot_id"`
	Semantic   SemanticFrozenNodes `json:"v27_semantic"`
}

// CompileSemanticDraftConfig freezes deterministic transfer node identities
// and explicit protection refs. Callers must first qualify/authorize all pinned
// documents and register the full input closure; this creates no graph/run.
func CompileSemanticDraftConfig(project, snapshot, session, run uuid.UUID, pins []SemanticProtectionPin) (SemanticDraftConfig, json.RawMessage, string, error) {
	var empty SemanticDraftConfig
	if project == uuid.Nil || snapshot == uuid.Nil || session == uuid.Nil || run == uuid.Nil || len(pins) < 1 || len(pins) > 5000 {
		return empty, nil, "", ErrInvalid
	}
	workflow, err := WorkflowID(run)
	if err != nil {
		return empty, nil, "", err
	}
	config := SemanticDraftConfig{QueueClass:"batch",ProjectID: project, SnapshotID: snapshot, Semantic: SemanticFrozenNodes{AuthSessionID: session, Nodes: make([]SemanticFrozenNode, len(pins))}}
	seen := make([]bool, len(pins))
	for _, pin := range pins {
		if pin.Ordinal < 0 || pin.Ordinal >= len(pins) || seen[pin.Ordinal] || !validRef(pin.Params) || !validRef(pin.SourceProtection) || !validRef(pin.TargetProtection) ||
			pin.Params.ArtifactID == pin.SourceProtection.ArtifactID || pin.Params.ArtifactID == pin.TargetProtection.ArtifactID ||
			pin.SourceProtection.ArtifactID == pin.TargetProtection.ArtifactID && pin.SourceProtection.SHA256 != pin.TargetProtection.SHA256 {
			return empty, nil, "", ErrInvalid
		}
		seen[pin.Ordinal] = true
		task, err := TaskID(workflow, "semantic.transfer", pin.Ordinal)
		if err != nil {
			return empty, nil, "", err
		}
		config.Semantic.Nodes[pin.Ordinal] = SemanticFrozenNode{TaskID: task, ParamsArtifactID: pin.Params.ArtifactID, ParamsSHA: pin.Params.SHA256, SourceProtection: pin.SourceProtection, TargetProtection: pin.TargetProtection}
	}
	body, err := canonical.Marshal(config)
	if err != nil || len(body) > 8*1024*1024 {
		return empty, nil, "", ErrInvalid
	}
	digest, err := canonical.Hash(config)
	if err != nil {
		return empty, nil, "", err
	}
	return config, body, digest, nil
}
