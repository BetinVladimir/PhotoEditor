package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
)

// BindSemanticProposalTasks binds compiled transfer inventory to a published
// user intent. Scope, qualification and SQL admission remain separate gates.
func BindSemanticProposalTasks(intentRef Ref, raw json.RawMessage, tasks []FrozenSemanticTask) error {
	if !validRef(intentRef) || len(tasks) < 1 || len(tasks) > 5000 {
		return ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != intentRef.SHA256 {
		return ErrInvalid
	}
	intent, err := DecodeSemanticEditIntent(raw)
	if err != nil {
		return err
	}
	seen := make(map[int]bool, len(tasks))
	for _, task := range tasks {
		if _, err := task.DraftTask(); err != nil {
			return err
		}
		ordinal := task.protection.Ordinal
		if ordinal < 0 || ordinal >= len(tasks) || seen[ordinal] || task.snapshotID != intent.TargetSnapshotID {
			return ErrInvalid
		}
		seen[ordinal] = true
		params, err := DecodeSemanticTransferParams(task.closure.ParamsJCS)
		if err != nil || params.Intent != intentRef || params.Reference != intent.Reference {
			return ErrInvalid
		}
		selected := false
		for _, entry := range task.closure.Direct {
			if entry == intent.ReferenceMask {
				selected = true
				break
			}
		}
		if !selected {
			return ErrInvalid
		}
	}
	return nil
}

// CompileSemanticProposalConfig combines user-intent binding, complete target
// coverage, deterministic graph identity and server-selected qualification.
// Publication, live catalog locks and quota admission must precede activation.
func CompileSemanticProposalConfig(project, session, run uuid.UUID, intentRef Ref, intentBody json.RawMessage, snapshotRef Ref, snapshotBody json.RawMessage, tasks []FrozenSemanticTask, qualification SemanticQualificationPin) (SemanticDraftConfig, json.RawMessage, string, error) {
	if err := BindSemanticProposalTasks(intentRef, intentBody, tasks); err != nil {
		return SemanticDraftConfig{}, nil, "", err
	}
	return CompileQualifiedSemanticSnapshotConfig(project, session, run, snapshotRef, snapshotBody, tasks, qualification)
}
