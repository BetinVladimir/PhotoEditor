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

type restorationTaskDescriptor struct {
	SchemaVersion int                     `json:"schema_version"`
	Task          v2input.DraftTask       `json:"task"`
	Entries       []v2input.Entry         `json:"entries"`
	Params        json.RawMessage         `json:"params"`
	Origin        RestorationManualOrigin `json:"origin"`
}

// Descriptor is immutable recovery data. The original rasterizer locators
// must be replayed under live SQL authority before a task can be admitted.
func (d RestorationResolvedDraft) Descriptor(paramsRaw []byte) ([]byte, error) {
	if !isDocumentRef(d.Origin.Root) || !isDocumentRef(d.Origin.Params) ||
		!isDocumentRef(d.Origin.TargetPixel) || !isDocumentRef(d.Origin.ProtectedPixel) ||
		d.Origin.Root == d.Origin.Params || d.Draft.Task.ID == uuid.Nil ||
		len(d.Draft.Entries) < 6 || len(d.Draft.Entries) > 127 ||
		len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return nil, v2input.ErrInvalid
	}
	params, err := canonical.Bytes(paramsRaw)
	if err != nil || !bytes.Equal(params, paramsRaw) {
		return nil, v2input.ErrInvalid
	}
	return canonical.Marshal(restorationTaskDescriptor{SchemaVersion: 1,
		Task: d.Draft.Task, Entries: append([]v2input.Entry(nil), d.Draft.Entries...),
		Params: append(json.RawMessage(nil), paramsRaw...), Origin: d.Origin})
}

// RestoreRestorationTaskDescriptor recompiles using fresh catalog reads,
// original-producer SQL and supplied qualification pin, then compares every byte.
// A stored descriptor never grants source, mask, consent or worker authority.
func RestoreRestorationTaskDescriptor(ctx context.Context, raw []byte,
	db v2portrait.DonorConfirmationTransactions, who auth.Identity,
	tenant, project, snapshot, workflow uuid.UUID, ordinal int,
	operation string, paramsRaw, baseProfile []byte,
	pin v2portrait.RestorationQualificationPin, paramsRef v2input.Ref,
	inventory []v2input.Entry, baseCatalog, donorCatalog ArtifactCatalog,
	objects v2input.SessionAnalysisObjectReader) (RestorationResolvedDraft, error) {
	var zero RestorationResolvedDraft
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalRaw, raw) {
		return zero, v2input.ErrInvalid
	}
	var stored restorationTaskDescriptor
	if json.Unmarshal(raw, &stored) != nil || stored.SchemaVersion != 1 ||
		!bytes.Equal(stored.Params, paramsRaw) {
		return zero, v2input.ErrInvalid
	}
	current, err := CompileResolvedRestorationDraftTask(ctx, db, who, tenant,
		project, snapshot, workflow, ordinal, operation, paramsRaw, baseProfile,
		pin, paramsRef, inventory, baseCatalog, donorCatalog, objects)
	if err != nil {
		return zero, err
	}
	expected, err := current.Descriptor(paramsRaw)
	if err != nil || !bytes.Equal(expected, raw) {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return current, nil
}
