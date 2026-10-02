package v2input

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// SQLLensDraftStore reserves output quota and freezes the document plan before
// publication. It uses scheduler credentials and never seals or dispatches.
type SQLLensDraftStore struct{ DB AnalysisStageQuerier }

func (s SQLLensDraftStore) Begin(ctx context.Context, tenant, actor, run uuid.UUID, policySHA string, draft CompiledLensDraft) error {
	if s.DB == nil || tenant == uuid.Nil || actor == uuid.Nil || run == uuid.Nil || !digestPattern.MatchString(policySHA) || draft.OutputBytes < 1 {
		return ErrInvalid
	}
	body, err := canonical.Marshal(draft.Config)
	if err != nil || !bytes.Equal(body, draft.Body) {
		return ErrInvalid
	}
	sha, err := canonical.Hash(draft.Config)
	if err != nil || sha != draft.SHA256 {
		return ErrInvalid
	}
	workflow, err := WorkflowID(run)
	if err != nil {
		return err
	}
	var accepted bool
	err = s.DB.QueryRow(ctx, `SELECT photo.begin_v25_lens_draft($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		tenant, draft.Config.ProjectID, draft.Config.SnapshotID, actor, draft.Config.Lens.AuthSessionID,
		run, workflow, string(body), policySHA, draft.OutputBytes).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return ErrExpired
	}
	return nil
}

// Seal activates only already-registered tasks. The SQL graph guard repeats
// frozen-selection and live authority checks before the existing closure,
// registry, quota and consent gates; it performs no artifact publication.
func (s SQLLensDraftStore) Seal(ctx context.Context, tenant uuid.UUID, prepared PreparedLensDraft) error {
	if s.DB == nil || tenant == uuid.Nil || prepared.RunID == uuid.Nil || len(prepared.Tasks) < 1 || len(prepared.Tasks) > 5000 {
		return ErrInvalid
	}
	workflow, err := WorkflowID(prepared.RunID)
	if err != nil || workflow != prepared.WorkflowID {
		return ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, task := range prepared.Tasks {
		if !validTaskIdentity(workflow, task.Graph.ID, task.NodeKey) || seen[task.Graph.ID] ||
			!validBuilt(task.Graph.Built, task.Graph.Direct) || !validRef(task.Params) || !digestPattern.MatchString(task.InputDigest) {
			return ErrInvalid
		}
		seen[task.Graph.ID] = true
	}
	var sealed bool
	if err = s.DB.QueryRow(ctx, `SELECT photo.seal_v2_graph($1,$2)`, tenant, prepared.RunID).Scan(&sealed); err != nil {
		return err
	}
	if !sealed {
		return ErrInvalid
	}
	return nil
}
