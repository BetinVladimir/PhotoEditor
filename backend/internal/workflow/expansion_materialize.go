package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// Materialize commits a prepared branch under its current SQL lease. Success
// means durable materialization, not dispatch readiness: graph remains unsealed
// and a repair task remains blocked until continuation dependencies are bound.
func (s SQLExpansionLeases) Materialize(ctx context.Context, lease DefectExpansionLease, branch MaterializedDefectBranch, task *PreparedDefectTask) (bool, error) {
	invalid := errors.New("invalid defect branch commit")
	if s.DB == nil || lease.ExpansionID == uuid.Nil || lease.Token == uuid.Nil || lease.RunID == uuid.Nil || lease.ReviewID == uuid.Nil || branch.Route != lease.Route {
		return false, invalid
	}
	if _, err := domain.ParseDigest(lease.DecisionHash); err != nil {
		return false, invalid
	}
	switch branch.Route {
	case "repair":
		if task == nil || branch.Params == nil || branch.Selection == nil || branch.BaseImage != nil || task.Params != *branch.Params || !reflect.DeepEqual(task.Inputs, branch.Inputs) || len(task.Inputs) > 128 {
			return false, invalid
		}
		key := "defects.repair/review/" + lease.ReviewID.String()
		if task.NodeKey != key || task.ID != uuid.NewSHA1(lease.RunID, []byte(key)) || task.Operation != "defects.repair" || task.Version != "1" || task.ResourceClass != "cpu" {
			return false, invalid
		}
		expectedDigest, digestErr := repairInputDigest(task.Operation, task.Version, task.Params, task.Inputs)
		if digestErr != nil || expectedDigest != task.InputDigest {
			return false, invalid
		}
		if _, err := ParseResourceEstimate(task.ExecutionProfile); err != nil {
			return false, invalid
		}
		raw, err := canonical.Marshal(lease.Selection)
		if err != nil {
			return false, invalid
		}
		h := sha256.Sum256(raw)
		if branch.Selection.SHA256 != hex.EncodeToString(h[:]) {
			return false, invalid
		}
	case "base":
		if task != nil || branch.Params != nil || branch.Selection != nil || branch.BaseImage == nil || *branch.BaseImage != lease.Selection.Image || len(branch.Inputs) != 0 {
			return false, invalid
		}
	case "skip":
		if task != nil || branch.Params != nil || branch.Selection != nil || branch.BaseImage != nil || len(branch.Inputs) != 0 {
			return false, invalid
		}
	default:
		return false, invalid
	}
	payload, err := canonical.Marshal(struct {
		Route        string              `json:"route"`
		DecisionHash string              `json:"decision_hash"`
		Base         *review.ArtifactRef `json:"base,omitempty"`
		Selection    *review.ArtifactRef `json:"selection,omitempty"`
		Task         *PreparedDefectTask `json:"task,omitempty"`
	}{branch.Route, lease.DecisionHash, branch.BaseImage, branch.Selection, task})
	if err != nil || len(payload) > 1048576 {
		return false, invalid
	}
	var done bool
	err = s.DB.QueryRow(ctx, "SELECT photo.materialize_defect_branch($1,$2,$3)", lease.ExpansionID, lease.Token, payload).Scan(&done)
	return done, err
}
