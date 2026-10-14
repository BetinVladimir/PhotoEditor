package v2draft

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type ReflectionRestorationCoordinator struct {
	CatalogProject   uuid.UUID
	BaseAssetVersion uuid.UUID
	DB               v2portrait.DonorConfirmationTransactions
	ParamsPublisher  v2input.DocumentPublisher
	Transport        v2input.DocumentPublisher
	BaseCatalog      ArtifactCatalog
	Objects          v2input.SessionAnalysisObjectReader
}

type ReflectionRestorationSubmission struct {
	TaskID    uuid.UUID
	ParamsRef v2input.Ref
	Params    []byte
}

// Admit is the internal reflection graph path. It never returns a public job
// ticket; the later receipt reader must independently prove committed SQL state.
func (c ReflectionRestorationCoordinator) Admit(ctx context.Context,
	who auth.Identity, tenant, project, run, workflow, snapshot uuid.UUID,
	ordinal int, paramsProposal, baseProfile []byte,
	pin v2portrait.RestorationQualificationPin) (ReflectionRestorationSubmission, error) {
	var zero ReflectionRestorationSubmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.ParamsPublisher == nil || c.Transport == nil ||
		c.BaseCatalog == nil || c.Objects == nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		tenant == uuid.Nil || project == uuid.Nil || run == uuid.Nil ||
		workflow == uuid.Nil || snapshot == uuid.Nil ||
		c.CatalogProject != project || c.BaseAssetVersion == uuid.Nil ||
		pin.Operation != "image.reflection" {
		return zero, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return zero, err
	}
	params, err := canonical.Bytes(paramsProposal)
	if err != nil || !bytes.Equal(params, paramsProposal) ||
		len(params) < 2 || len(params) > 65536 {
		return zero, v2input.ErrInvalid
	}
	var selected struct {
		Base         v2input.SessionVersionBinding `json:"base"`
		ModelLockSHA string                        `json:"model_lock_sha"`
		Calibration  v2input.Ref                   `json:"calibration"`
	}
	if json.Unmarshal(params, &selected) != nil ||
		selected.Base.AssetVersionID != c.BaseAssetVersion ||
		selected.ModelLockSHA != pin.ModelLockSHA ||
		selected.Calibration != pin.Calibration {
		return zero, v2input.ErrInvalid
	}
	if _, err := v2input.TaskID(workflow, "image.reflection", ordinal); err != nil {
		return zero, err
	}
	if _, err := v2portrait.CompilePinnedRestorationProfile("image.reflection",
		params, baseProfile, pin); err != nil {
		return zero, err
	}
	material, err := ResolveRestorationManualEvidence(ctx, c.DB, who,
		tenant, project, snapshot, "image.reflection", params,
		c.BaseCatalog, nil, c.Objects)
	if err != nil {
		return zero, err
	}
	paramsRef, err := (SQLRestorationParamsPublisher{DB: c.DB,
		Publisher: c.ParamsPublisher}).PublishReflection(ctx, who, tenant,
		run, workflow, ordinal, params, pin, material.Origin)
	if err != nil {
		return zero, err
	}
	inventory, err := v2portrait.BuildRestorationInputInventory(ctx,
		"image.reflection", params, paramsRef, pin, material.Documents)
	if err != nil {
		return zero, err
	}
	prepared, err := CompileResolvedRestorationDraftTask(ctx, c.DB, who,
		tenant, project, snapshot, workflow, ordinal, "image.reflection",
		params, baseProfile, pin, paramsRef, inventory,
		c.BaseCatalog, nil, c.Objects)
	if err != nil {
		return zero, err
	}
	store := SQLRestorationDescriptorStore{DB: c.DB}
	if _, err := store.PublishAndRegisterTransport(ctx, who, tenant, run,
		prepared, params, c.Transport); err != nil {
		return zero, err
	}
	if err := store.AdmitRegistered(ctx, who, tenant, run, prepared, params); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ReflectionRestorationSubmission{TaskID: prepared.Draft.Task.ID,
		ParamsRef: paramsRef, Params: append([]byte(nil), params...)}, nil
}
