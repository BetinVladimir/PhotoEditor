package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// prepareApplyMaterialization is the pure boundary between authored phase
// Params and durable task materialization. It creates no authority or receipts.
// Both normal Apply and operator measurement consume this exact task ABI.
func prepareApplyMaterialization(reg Registry, plan applyNodePlan, ref review.ArtifactRef, profile json.RawMessage) (PreparedTask, error) {
	o, found := reg[plan.Operation]
	if !found {
		return PreparedTask{}, errors.New("apply operation missing from registry")
	}
	if plan.ID == uuid.Nil || plan.NodeKey == "" || ref.ID == uuid.Nil {
		return PreparedTask{}, errors.New("apply materialization identity invalid")
	}
	raw, err := canonical.Bytes(plan.Params)
	if err != nil || !bytes.Equal(raw, plan.Params) {
		return PreparedTask{}, errors.New("apply params must be canonical")
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if plan.ParamsSHA != digest || ref.SHA256 != digest {
		return PreparedTask{}, errors.New("apply params publication mismatch")
	}
	if !json.Valid(profile) {
		return PreparedTask{}, errors.New("apply profile invalid")
	}
	task := PreparedTask{ID: plan.ID, NodeKey: plan.NodeKey, Operation: plan.Operation, Version: o.Version, ResourceClass: o.ResourceClass, TimeoutSeconds: o.TimeoutSeconds, Params: ref, Inputs: append([]review.ArtifactRef(nil), plan.Inputs...), ExecutionProfile: append(json.RawMessage(nil), profile...)}
	task.InputDigest, err = repairInputDigest(task.Operation, o.Version, task.Params, task.Inputs)
	return task, err
}
