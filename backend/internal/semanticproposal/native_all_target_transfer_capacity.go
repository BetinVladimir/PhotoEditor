package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Pure recovery inputs, never an admission capability. Uses saved limits/costs
// exclusively; exact reference proof must have been independently reread first.
// Full target reconstruction and final live SQL admission remain mandatory.
func (s SQLTransferSourceReader) restoreNativeAllTargetTransferCapacity(ctx context.Context, who auth.Identity, request Request, raw json.RawMessage, sha string, reference nativeInputTransferReferenceDocuments, color TransferColorProfile) (TransferPreparation, error) {
	var zero TransferPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	saved, err := decodeNativeAllTargetTransferConfig(raw, sha)
	if err != nil {
		return zero, err
	}
	marker := saved.Native
	if saved.ProjectID != s.ProjectID || saved.SnapshotID != request.Intent.TargetSnapshotID || saved.Semantic.AuthSessionID != who.SessionID || request.ActorID != who.UserID || marker.Parent != request.RunID || marker.RequestSHA != request.RequestSHA || marker.Reference != request.Intent.Reference || !reference.person.originalPinned {
		return zero, v2input.ErrConflict
	}
	registered := reference.person.source.protection
	p := registered.published.preparation
	if len(p.plan.shell.Tasks) != 1 || p.plan.prepared.dependencies.preparation.person == nil {
		return zero, v2input.ErrInvalid
	}
	original := p.plan.prepared.dependencies.preparation.landmarksPlan.detector
	snapshot, err := v2input.DecodePinnedSessionInputSnapshot(original.anchor.snapshotRef, original.anchor.snapshotBody)
	if err != nil {
		return zero, err
	}
	if marker.Intent != original.published.Ref || *saved.Semantic.Snapshot != original.anchor.snapshotRef || saved.Semantic.SnapshotDigest != snapshot.SnapshotDigest {
		return zero, v2input.ErrConflict
	}
	manifest, err := compileNativeTargetPreparationManifest(ctx, who, request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody)
	if err != nil {
		return zero, err
	}
	if manifest.sha != marker.ManifestSHA {
		return zero, v2input.ErrConflict
	}
	var inventory nativeTargetPreparationManifestBody
	if json.Unmarshal(manifest.body, &inventory) != nil || len(inventory.Entries) != len(marker.Targets) {
		return zero, v2input.ErrInvalid
	}
	for i, t := range marker.Targets {
		e := inventory.Entries[i]
		if t.Ordinal != e.Ordinal || t.Binding != e.Binding || t.Namespace != e.Namespace || t.ReuseReference != e.ReuseReference {
			return zero, v2input.ErrConflict
		}
	}
	producer := nativeAllTargetTransferProducer{Run: p.plan.shell.Run, Task: p.plan.shell.Tasks[0].ID, Result: p.result.catalog.ResultID, Params: p.result.catalog.Params}
	person, err := nativeDefectPersonRecoveryFromProof(ctx, *p.plan.prepared.dependencies.preparation.person)
	if err != nil {
		return zero, err
	}
	if producer != marker.ReferenceProducer || nativeTransferProtectionLocator(s.Bucket, registered.objects[0]) != marker.ReferenceProtection || marker.ReferencePixels != registered.verified.Pixels || !reflect.DeepEqual(person, marker.OriginalPerson) {
		return zero, v2input.ErrConflict
	}
	var expected, personExpected v2qualification.Expected
	if json.Unmarshal(marker.Capacity.Expected, &expected) != nil || json.Unmarshal([]byte(reference.person.person.expected), &personExpected) != nil {
		return zero, v2input.ErrInvalid
	}
	capacity := marker.Capacity
	c := TransferPreparation{legacyAllTargetQueueOmitted: saved.QueueClass == "",Qualification: *saved.Semantic.Qualification, Expected: expected, Color: color, Costs: capacity.Costs, RAM: capacity.RAM, VRAM: capacity.VRAM, Scratch: capacity.Scratch, Output: capacity.Output, MetadataBytes: capacity.Metadata, SourceProtection: marker.ReferenceProtection.Ref, PersonProtection: &TransferPersonProtection{Report: marker.OriginalPerson.Report, Expected: personExpected}, TargetProtection: make([]v2input.Ref, len(marker.Targets))}
	for i, target := range marker.Targets {
		c.TargetProtection[i] = target.Protection.Ref
	}
	if err := validateTransferConfiguration(c); err != nil {
		return zero, err
	}
	if err := s.validateNativeInputTransferReference(ctx, who, request, c, reference); err != nil {
		return zero, err
	}
	return ownTransferPreparation(c), ctx.Err()
}
