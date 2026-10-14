package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent lock. Only server-loaded request/intent/snapshot
// documents are inputs; all private original proofs are reconstructed here.
func (p TransferDraftPreparer) restoreNativeAllTargetTransferOriginal(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, color TransferColorProfile) (detectorDraftPlan, nativeInputTransferReferenceDocuments, v2input.Built, error) {
	var zero detectorDraftPlan
	var ref nativeInputTransferReferenceDocuments
	var transport v2input.Built
	if err := ctx.Err(); err != nil {
		return zero, ref, transport, err
	}
	if catalog == nil || p.Scheduler == nil {
		return zero, ref, transport, v2input.ErrInvalid
	}
	raw, sha, err := p.Reader.readNativeAllTargetTransferSaved(ctx, who, request)
	if err != nil {
		return zero, ref, transport, err
	}
	saved, err := p.Reader.decodeNativeAllTargetTransferSaved(ctx, who, request, raw, sha)
	if err != nil {
		return zero, ref, transport, err
	}
	if saved.Native.Intent != published.Ref || saved.Semantic.Snapshot == nil || *saved.Semantic.Snapshot != snapshot {
		return zero, ref, transport, v2input.ErrConflict
	}
	locator, params, err := p.Reader.readNativeInputDefectSavedParams(ctx, who, request, published.Ref, snapshot)
	if err != nil {
		return zero, ref, transport, err
	}
	if locator.person == nil {
		return zero, ref, transport, v2input.ErrConflict
	}
	var values struct {
		Diameter [2]float64 `json:"diameter_face_fraction"`
	}
	if json.Unmarshal(params, &values) != nil || values.Diameter[0] <= 0 || values.Diameter[1] < values.Diameter[0] {
		return zero, ref, transport, v2input.ErrInvalid
	}
	producers, err := p.Reader.restoreNativeInputFaceProducers(ctx, catalog, who, request, key, published, snapshot, snapshotBody, color)
	if err != nil {
		return zero, ref, transport, err
	}
	defect, err := p.Reader.restoreNativeInputDefectDraftPlan(ctx, p.Scheduler, who, producers, color, values.Diameter, nil, nil, nil, nil)
	if err != nil {
		return zero, ref, transport, err
	}
	transport, err = recoverNativeInputDefectTransport(ctx, p.Scheduler, who, defect)
	if err != nil {
		return zero, ref, v2input.Built{}, err
	}
	registered, err := p.Reader.restoreRegisteredNativeInputProtection(ctx, who, defect, color)
	if err != nil {
		return zero, ref, v2input.Built{}, err
	}
	if registered.verified.Root != saved.Native.ReferenceProtection.Ref {
		return zero, ref, v2input.Built{}, v2input.ErrConflict
	}
	_, source, err := p.Reader.readQualifiedSourcePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, color, false)
	if err != nil {
		return zero, ref, v2input.Built{}, err
	}
	person, err := p.Reader.prepareNativeInputReferencePersonMode(ctx, who, request, source, registered, color, saved.Native.OriginalPerson.Report, true)
	if err != nil {
		return zero, ref, v2input.Built{}, err
	}
	ref, err = p.Reader.readNativeInputTransferReferenceDocuments(ctx, who, request, person, color, saved.Native.Capacity.Metadata)
	if err != nil {
		return zero, nativeInputTransferReferenceDocuments{}, v2input.Built{}, err
	}
	if _, err := p.Reader.restoreNativeAllTargetTransferCapacity(ctx, who, request, raw, sha, ref, color); err != nil {
		return zero, nativeInputTransferReferenceDocuments{}, v2input.Built{}, err
	}
	return producers.detector, ref, transport, ctx.Err()
}
