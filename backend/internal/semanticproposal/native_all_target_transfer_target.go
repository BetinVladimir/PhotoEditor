package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

// A native target cannot become generic person/protection authority. Reference
// reuse retains the already verified reference; other targets retain their own
// canonical defect plan and registered immutable root for the final SQL barrier.
type nativeTransferTargetProof struct {
	ordinal        int
	binding        v2input.SessionVersionBinding
	source         sourcePreparationProof
	image          string
	protection     VerifiedProtection
	reuseReference bool
	plan           *nativeTargetDefectDraftPlan
	registered     registeredNativeTargetDefectProtection
	completed      string
	seal           string
}

func nativeTransferTargetDigest(p nativeTransferTargetProof) (string, error) {
	plan := ""
	if p.plan != nil {
		plan = p.plan.seal
	}
	source := p.source
	objects := make([]any, 6)
	for i, o := range p.registered.published.preparation.completed.objects {
		objects[i] = map[string]any{"ref": o.ref, "object": o.object}
	}
	return canonical.Hash(map[string]any{"ordinal": p.ordinal, "binding": p.binding, "source": map[string]any{"tenant": source.tenant, "project": source.project, "actor": source.actor, "session": source.session, "bucket": source.bucket, "binding": source.binding, "icc": source.iccSHA, "unit_rgb": source.unitRGB, "pins": source.pins}, "image": p.image, "protection": p.protection, "reuse_reference": p.reuseReference, "completed": p.completed, "plan": plan, "registered": map[string]any{"object": map[string]any{"ref": p.registered.object.ref, "object": p.registered.object.object}, "root": p.registered.published.root, "verified": p.registered.verified, "document": p.registered.published.preparation.document, "body": p.registered.published.preparation.body, "catalog": p.registered.published.preparation.completed.catalog, "evidence": p.registered.published.preparation.completed.evidence, "objects": objects}})
}

func (s SQLTransferSourceReader) verifyNativeTransferTarget(ctx context.Context, who auth.Identity, request Request, reference nativeInputTransferReferenceDocuments, p nativeTransferTargetProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest, err := nativeTransferTargetDigest(p)
	if err != nil {
		return err
	}
	if p.seal == "" || digest != p.seal || p.ordinal < 0 || p.ordinal >= 5000 {
		return fmt.Errorf("native transfer target proof mismatch: %w", v2input.ErrConflict)
	}
	if p.source.tenant != request.TenantID || p.source.project != s.ProjectID || p.source.actor != who.UserID || p.source.session != who.SessionID || p.source.bucket != s.Bucket || p.source.binding != p.binding || p.source.unitRGB == p.reuseReference {
		return v2input.ErrInvalid
	}
	if _, err := marshalSourceEvidencePins(p.source); err != nil {
		return err
	}
	pixels, err := v2input.ResolveSemanticMaskPixels(p.protection.Root, p.protection.Body, p.binding, []byte(p.image), true)
	if err != nil {
		return err
	}
	if pixels != p.protection.Pixels {
		return fmt.Errorf("native transfer target proof mismatch: %w", v2input.ErrConflict)
	}
	if p.reuseReference {
		if p.completed != "" {
			return v2input.ErrInvalid
		}
		if p.binding != request.Intent.Reference || p.source != reference.person.source.source || p.image != string(reference.reference) || p.plan != nil || !reflect.DeepEqual(p.registered, registeredNativeTargetDefectProtection{}) || p.protection.Root != reference.person.source.protection.verified.Root || p.protection.Pixels != reference.person.source.protection.verified.Pixels || string(p.protection.Body) != string(reference.protection) {
			return fmt.Errorf("native transfer target proof mismatch: %w", v2input.ErrConflict)
		}
		return ctx.Err()
	}
	if p.plan == nil || p.binding == request.Intent.Reference {
		return v2input.ErrInvalid
	}
	plan := *p.plan
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return err
	}
	a := plan.anchor
	if a.request.RunID != request.RunID || a.request.RequestSHA != request.RequestSHA || a.request.ActorID != who.UserID || a.entry.Ordinal != p.ordinal || a.entry.Binding != p.binding || a.entry.ReuseReference || p.source != plan.prepared.preparation.personPlan.person.prepared.source || p.image != plan.prepared.preparation.landmarks.detector.completed.sourceBody {
		return fmt.Errorf("native transfer target producer/source mismatch: %w", v2input.ErrConflict)
	}
	r := p.registered
	prepared := r.published.preparation
	if len(prepared.completed.catalog.Outputs) != 5 || prepared.document != targetDefectProtectionDocument(a.entry.Namespace) || r.object.ref != r.published.root || r.object.object.SHA256 != r.object.ref.SHA256 || r.verified.Root != r.object.ref || r.verified.Pixels != prepared.completed.catalog.Outputs[1] || string(r.verified.Body) != prepared.body || !reflect.DeepEqual(p.protection, r.verified) {
		return fmt.Errorf("native transfer target proof mismatch: %w", v2input.ErrConflict)
	}
	expected, pixels, err := v2input.BuildSemanticDefectProtectionDocument(ctx, prepared.completed.catalog.Params, prepared.completed.evidence.Params, prepared.completed.catalog.Outputs[4], prepared.completed.evidence.Report, prepared.completed.catalog.Outputs[3], prepared.completed.evidence.Proposal, p.binding, []byte(p.image))
	if err != nil {
		return err
	}
	if string(expected) != prepared.body || pixels != p.protection.Pixels || prepared.completed.catalog.Params != plan.prepared.params.ref || string(prepared.completed.evidence.Params) != plan.prepared.preparation.params.params {
		return fmt.Errorf("native transfer target proof mismatch: %w", v2input.ErrConflict)
	}
	if !reflect.DeepEqual(plan.prepared.preparation.personPlan.person.policy, reference.person.policy) {
		return fmt.Errorf("native transfer target person policy mismatch: %w", v2input.ErrConflict)
	}
	return ctx.Err()
}

// Caller owns original parent lock. Restore never republishes target roots or
// accepts a source-wide latest candidate. No partial target list escapes.
func (p TransferDraftPreparer) restoreNativeTransferTargets(ctx context.Context, who auth.Identity, original detectorDraftPlan, manifest nativeTargetPreparationManifest, reference nativeInputTransferReferenceDocuments, catalogs NativeParentArtifactCatalogFactory) ([]nativeTransferTargetProof, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("native transfer restore initialization: %w", err)
	}
	if catalogs == nil || p.Configuration.MetadataBytes <= 0 {
		return nil, v2input.ErrInvalid
	}
	if err := p.Reader.validateNativeInputTransferReference(ctx, who, original.request, p.Configuration, reference); err != nil {
		return nil, fmt.Errorf("native transfer validateNativeInputTransferReference: %w", err)
	}
	owned, err := restoreNativeTargetPreparationManifest(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, manifest.body, manifest.sha)
	if err != nil {
		return nil, fmt.Errorf("native transfer validateNativeInputTransferReference: %w", err)
	}
	before, err := ReadInventory(ctx, p.Reader.DB, who, original.request)
	if err != nil {
		return nil, fmt.Errorf("native transfer validateNativeInputTransferReference: %w", err)
	}
	if !sameTransferInventory(before, original.published.Inventory) {
		return nil, v2input.ErrConflict
	}
	var inventory nativeTargetPreparationManifestBody
	if json.Unmarshal(owned.body, &inventory) != nil || len(inventory.Entries) != len(before.Targets) {
		return nil, v2input.ErrConflict
	}
	var retained int64
	if err := chargeMetadata(&retained, p.Configuration.MetadataBytes, len(inventory.Entries)*1024, len(owned.body)); err != nil {
		return nil, fmt.Errorf("native transfer validateNativeInputTransferReference: %w", err)
	}
	out := make([]nativeTransferTargetProof, len(inventory.Entries))
	for i, entry := range inventory.Entries {
		if entry.Ordinal != i || entry.Binding != before.Targets[i] {
			return nil, v2input.ErrConflict
		}
		proof := nativeTransferTargetProof{ordinal: i, binding: entry.Binding, reuseReference: entry.ReuseReference}
		if entry.ReuseReference {
			proof.source = reference.person.source.source
			proof.image = string(reference.reference)
			proof.protection = VerifiedProtection{Root: reference.person.source.protection.verified.Root, Pixels: reference.person.source.protection.verified.Pixels, Body: append(json.RawMessage(nil), reference.protection...)}
		} else {
			upstream, err := p.restoreNativeTargetDefectPrerequisites(ctx, who, original, owned, i, catalogs)
			if err != nil {
				return nil, fmt.Errorf("native transfer restoreNativeTargetDefectPrerequisites: %w", err)
			}
			anchor, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, owned, i, "defects.propose")
			if err != nil {
				return nil, fmt.Errorf("native transfer restoreNativeTargetDefectPrerequisites: %w", err)
			}
			saved, err := p.Reader.readNativeTargetDefectSaved(ctx, who, anchor)
			if err != nil {
				return nil, fmt.Errorf("native transfer readNativeTargetDefectSaved: %w", err)
			}
			if saved.State != "saved" || saved.RunState != "completed" {
				return nil, v2input.ErrConflict
			}
			plan, err := p.Reader.restoreNativeTargetDefectPlan(ctx, who, upstream.landmarks, upstream.skin, upstream.person, anchor, p.Configuration.Color, saved)
			if err != nil {
				return nil, fmt.Errorf("native transfer restoreNativeTargetDefectPlan: %w", err)
			}
			prepared, err := p.Reader.prepareNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color)
			if err != nil {
				return nil, fmt.Errorf("native transfer prepareNativeTargetDefectProtection: %w", err)
			}
			object, err := p.Reader.nativeTargetDefectProtectionObject(ctx, who, plan)
			if err != nil {
				return nil, fmt.Errorf("native transfer nativeTargetDefectProtectionObject: %w", err)
			}
			registered, err := p.Reader.readRegisteredNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color, publishedNativeTargetDefectProtection{preparation: prepared, root: object.ref})
			if err != nil {
				return nil, fmt.Errorf("native transfer readRegisteredNativeTargetDefectProtection: %w", err)
			}
			dependencies, err := p.Reader.prepareNativeTargetDefectDependencies(ctx, who, original, plan.prepared.preparation, p.Configuration.Color, upstream.transports)
			if err != nil {
				return nil, err
			}
			var built v2input.Built
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				var err error
				built, err = p.Reader.recoverNativeTargetDefectTransport(ctx, tx, who, original, plan)
				return err
			}); err != nil {
				return nil, err
			}
			packet, err := p.Reader.marshalNativeTargetDefectCompletedEvidence(ctx, who, original, plan, dependencies, p.Configuration.Color, registered.published.preparation.completed, built)
			if err != nil {
				return nil, err
			}
			if err := chargeMetadata(&retained, p.Configuration.MetadataBytes, len(packet)); err != nil {
				return nil, err
			}
			proof.completed = string(packet)
			proof.plan = &plan
			proof.registered = registered
			proof.protection = registered.verified
			proof.source = plan.prepared.preparation.personPlan.person.prepared.source
			proof.image = plan.prepared.preparation.landmarks.detector.completed.sourceBody
			if err := chargeMetadata(&retained, p.Configuration.MetadataBytes, len(plan.shell.Config), len(plan.prepared.entries), len(plan.prepared.sizes), len(prepared.body), len(prepared.completed.evidence.Params), len(prepared.completed.evidence.Proposal), len(prepared.completed.evidence.Report)); err != nil {
				return nil, fmt.Errorf("native transfer readRegisteredNativeTargetDefectProtection: %w", err)
			}
		}
		if err := chargeMetadata(&retained, p.Configuration.MetadataBytes, len(proof.image), len(proof.source.pins), len(proof.protection.Body)); err != nil {
			return nil, fmt.Errorf("native transfer readRegisteredNativeTargetDefectProtection: %w", err)
		}
		proof.seal, err = nativeTransferTargetDigest(proof)
		if err != nil {
			return nil, fmt.Errorf("native transfer readRegisteredNativeTargetDefectProtection: %w", err)
		}
		if err := p.Reader.verifyNativeTransferTarget(ctx, who, original.request, reference, proof); err != nil {
			return nil, fmt.Errorf("native transfer verifyNativeTransferTarget: %w", err)
		}
		out[i] = proof
	}
	// Immutable object version pins are repeated after later target IO; source
	// proof rechecks retain its full qualified pixels/ICC authority.
	for _, proof := range out {
		if err := p.Reader.recheckSourcePreparationProof(ctx, who, original.request.TenantID, proof.binding, p.Configuration.Color, proof.source.unitRGB, proof.source); err != nil {
			return nil, fmt.Errorf("native transfer recheckSourcePreparationProof: %w", err)
		}
		if proof.plan != nil {
			object, err := p.Reader.nativeTargetDefectProtectionObject(ctx, who, *proof.plan)
			if err != nil {
				return nil, fmt.Errorf("native transfer nativeTargetDefectProtectionObject: %w", err)
			}
			if object != proof.registered.object {
				return nil, v2input.ErrConflict
			}
			result, err := p.Reader.nativeTargetDefectResultCatalog(ctx, who, *proof.plan)
			if err != nil {
				return nil, fmt.Errorf("native transfer nativeTargetDefectResultCatalog: %w", err)
			}
			if !reflect.DeepEqual(result, proof.registered.published.preparation.completed.catalog) {
				return nil, v2input.ErrConflict
			}
		}
	}
	after, err := ReadInventory(ctx, p.Reader.DB, who, original.request)
	if err != nil {
		return nil, fmt.Errorf("native transfer nativeTargetDefectResultCatalog: %w", err)
	}
	if !sameTransferInventory(before, after) {
		return nil, v2input.ErrConflict
	}
	current, err := p.Reader.readNativeInputTransferReferenceDocuments(ctx, who, original.request, reference.person, p.Configuration.Color, p.Configuration.MetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("native transfer readNativeInputTransferReferenceDocuments: %w", err)
	}
	if !sameNativeInputTransferDocuments(ctx, current, reference) {
		return nil, v2input.ErrConflict
	}
	return out, ctx.Err()
}
