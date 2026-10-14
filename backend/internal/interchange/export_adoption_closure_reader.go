package interchange

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// SchedulerExportNativeResolverFactory must install the same pinned native
// document policy and binary probes as export creation, scoped to sealed task
// inputs. A JSON-only or hash-only fallback is not accepted.
type SchedulerExportNativeResolverFactory interface {
	BuildSchedulerExportNativeResolver(context.Context, ExportAdoptionClaim,
		SchedulerExportResult, SQLSchedulerExportInputCatalog,
		ImportDocumentObjects) (ExportClosureNodeResolver, error)
}

type SchedulerExportClosureReader struct {
	Archive SchedulerExportArchiveReader
	Native  SchedulerExportNativeResolverFactory
}

// Prepare physically qualifies ZIP and the full portable source closure,
// then returns immutable evidence for AdoptScheduler. No DB write occurs here.
func (s SchedulerExportClosureReader) Prepare(ctx context.Context,
	claim ExportAdoptionClaim) (PreparedExportAdoption, FrozenExportDocuments, error) {
	var zero PreparedExportAdoption
	var noFrozen FrozenExportDocuments
	if err := ctx.Err(); err != nil {
		return zero, noFrozen, err
	}
	if claim.validate() != nil || s.Archive.ResultReader.Claims.DB == nil ||
		s.Archive.ResultReader.Objects == nil {
		return zero, noFrozen, v2input.ErrInvalid
	}
	if claim.Request.Format == "photoeditor_bundle" && s.Native == nil {
		return zero, noFrozen, v2input.ErrInvalid
	}
	result, archive, origins, err := s.Archive.ReadWithOrigins(ctx, claim)
	if err != nil {
		return zero, noFrozen, err
	}
	sources := []v2input.Entry{}
	if claim.Request.Format == "photoeditor_bundle" {
		if origins == nil {
			return zero, noFrozen, v2input.ErrInvalid
		}
		catalog := SQLSchedulerExportInputCatalog{Claims: s.Archive.ResultReader.Claims,
			Claim: claim, Bucket: s.Archive.ResultReader.Bucket}
		resolver, err := s.Native.BuildSchedulerExportNativeResolver(ctx, claim, result,
			catalog, s.Archive.ResultReader.Objects)
		if err != nil {
			return zero, noFrozen, err
		}
		if resolver == nil {
			return zero, noFrozen, v2input.ErrInvalid
		}
		if _, ok := resolver.(ExportGraphResolverFactory); !ok {
			return zero, noFrozen, v2input.ErrInvalid
		}
		inventory, err := ResolveExportBundleSourceInventory(ctx, result.Intent, result.Frozen, resolver)
		if err != nil {
			return zero, noFrozen, err
		}
		if err := ValidateExportBundleSourceInventoryWithPersonOrigins(ctx, result.Intent,
			result.Frozen, archive.Manifest(), inventory, origins.expected); err != nil {
			return zero, noFrozen, err
		}
		sources = inventory.Entries()
		if len(sources) == 0 {
			return zero, noFrozen, v2input.ErrInvalid
		}
	}
	latest, err := s.Archive.ResultReader.Read(ctx, claim)
	if err != nil {
		return zero, noFrozen, err
	}
	if latest.Intent.Identity != result.Intent.Identity ||
		!bytes.Equal(latest.Frozen.Body(), result.Frozen.Body()) ||
		latest.Root != result.Root || latest.Result.Root() != result.Result.Root() ||
		latest.Result.Bundle() != result.Result.Bundle() ||
		!sameExportRefs(latest.Result.Documents(), result.Result.Documents()) {
		return zero, noFrozen, v2input.ErrInvalid
	}
	bundle, err := (SQLSchedulerExportOutputCatalog{Claims: s.Archive.ResultReader.Claims,
		Claim: claim, Bucket: s.Archive.ResultReader.Bucket}).LookupExportOutput(ctx,
		v2input.Entry{Ref: archive.Bundle(), Contract: "InterchangeArchive"})
	if err != nil || bundle != archive.bundle.object {
		return zero, noFrozen, v2input.ErrInvalid
	}
	if origins != nil {
		claims := archive.PersonOrigins()
		if len(claims) != len(origins.expected) {
			return zero, noFrozen, v2input.ErrInvalid
		}
		for _, origin := range claims {
			if err := origins.current.RecheckExportPersonOrigin(ctx, origin); err != nil {
				return zero, noFrozen, err
			}
		}
	}
	prepared, err := freezeSchedulerExportAdoptionEvidence(ctx, claim,
		s.Archive.ResultReader.Bucket, latest, archive, sources)
	if err != nil {
		return zero, noFrozen, err
	}
	return prepared, latest.Frozen, ctx.Err()
}

func freezeSchedulerExportAdoptionEvidence(ctx context.Context, claim ExportAdoptionClaim,
	bucket string, result SchedulerExportResult, archive VerifiedExportArchive,
	sources []v2input.Entry) (PreparedExportAdoption, error) {
	var zero PreparedExportAdoption
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	wanted, err := exportIntentFromClaim(claim)
	if err != nil || result.Intent.Identity != wanted.Identity ||
		!bytes.Equal(result.Intent.RequestJCS, wanted.RequestJCS) ||
		result.Intent.RequestSHA != wanted.RequestSHA ||
		len(bucket) < 3 || len(bucket) > 63 ||
		(claim.Request.Format == "photoeditor_bundle" && len(sources) == 0) ||
		(claim.Request.Format != "photoeditor_bundle" && len(sources) != 0) {
		return zero, v2input.ErrInvalid
	}
	if _, _, err := DecodeFrozenExportDocuments(ctx, wanted, result.Frozen.Body()); err != nil {
		return zero, err
	}
	if !validImportRef(result.Result.Root()) || !validImportRef(archive.Bundle()) ||
		result.Result.Bundle() != archive.Bundle() || archive.bundle.object.Bytes < 22 ||
		archive.bundle.object.Key == "" || archive.bundle.object.VersionID == "" ||
		archive.bundle.object.VersionID == "null" ||
		archive.bundle.object.SHA256 != archive.Bundle().SHA256 ||
		len(archive.files) == 0 || len(archive.manifest) < 2 {
		return zero, v2input.ErrInvalid
	}
	evidence := exportAdoptionEvidence{Format: claim.Request.Format, Schema: 1,
		Tenant: claim.Tenant, Actor: claim.Actor, Identity: wanted.Identity,
		RequestSHA: claim.RequestSHA, FrozenSHA: rawSHA(result.Frozen.Body()),
		Root: result.Result.Root(), Documents: result.Result.Documents(),
		Bundle: archive.Bundle(), Bucket: bucket, ObjectKey: archive.bundle.object.Key,
		ObjectVersion: archive.bundle.object.VersionID, ArchiveBytes: archive.bundle.object.Bytes,
		ManifestSHA: rawSHA(archive.manifest), Files: archive.Files(),
		Sources: append([]v2input.Entry{}, sources...)}
	body, err := canonical.Marshal(evidence)
	if err != nil || len(body) > 16<<20 {
		return zero, v2input.ErrInvalid
	}
	return PreparedExportAdoption{body: body, sha: rawSHA(body),
		root: result.Result.Root(), bundle: archive.Bundle()}, ctx.Err()
}
