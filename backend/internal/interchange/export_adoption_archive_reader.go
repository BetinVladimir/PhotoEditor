package interchange

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/v2input"
)

// SchedulerExportPersonOriginFactory derives receipts and trust from sealed
// scheduler inputs, never from ZIP members or a borrowed user session.
type SchedulerExportPersonOriginFactory interface {
	BuildSchedulerExportPersonOrigins(context.Context, ExportAdoptionClaim, SchedulerExportResult,
		ImportDocumentObjects) (*ExportPersonOriginArchiveAuthority, error)
}

type SchedulerExportArchiveReader struct {
	ResultReader SchedulerExportResultReader
	Scratch      *ExportArchiveScratch
	Origins      SchedulerExportPersonOriginFactory
}

// Read verifies the exact canonical ZIP and manifest, then repeats JSON result
// qualification. Portable closure and transactional adoption remain separate.
func (s SchedulerExportArchiveReader) Read(ctx context.Context, claim ExportAdoptionClaim) (SchedulerExportResult, VerifiedExportArchive, error) {
	result, archive, _, err := s.ReadWithOrigins(ctx, claim)
	return result, archive, err
}

// ReadWithOrigins preserves the exact process-trusted expectation set used
// during ZIP verification for the later portable closure validation.
func (s SchedulerExportArchiveReader) ReadWithOrigins(ctx context.Context, claim ExportAdoptionClaim) (SchedulerExportResult, VerifiedExportArchive, *ExportPersonOriginArchiveAuthority, error) {
	var noResult SchedulerExportResult
	var noArchive VerifiedExportArchive
	if err := ctx.Err(); err != nil {
		return noResult, noArchive, nil, err
	}
	if s.Scratch == nil || s.ResultReader.Objects == nil || claim.validate() != nil {
		return noResult, noArchive, nil, v2input.ErrInvalid
	}
	if claim.Request.Format == "photoeditor_bundle" && s.Origins == nil {
		return noResult, noArchive, nil, v2input.ErrInvalid
	}
	result, err := s.ResultReader.Read(ctx, claim)
	if err != nil {
		return noResult, noArchive, nil, err
	}
	var origins *ExportPersonOriginArchiveAuthority
	if claim.Request.Format == "photoeditor_bundle" {
		origins, err = s.Origins.BuildSchedulerExportPersonOrigins(ctx, claim, result, s.ResultReader.Objects)
		if err != nil {
			return noResult, noArchive, nil, err
		}
		if origins == nil {
			return noResult, noArchive, nil, v2input.ErrInvalid
		}
	}
	catalog := SQLSchedulerExportOutputCatalog{Claims: s.ResultReader.Claims, Claim: claim, Bucket: s.ResultReader.Bucket}
	archive, err := (ExportArchiveReader{Catalog: catalog, Objects: s.ResultReader.Objects,
		Scratch: s.Scratch, PersonOrigins: origins}).Read(ctx, result.Intent, result.Frozen, result.Result.Bundle())
	if err != nil {
		return noResult, noArchive, nil, err
	}
	latest, err := s.ResultReader.Read(ctx, claim)
	if err != nil {
		return noResult, noArchive, nil, err
	}
	if latest.Intent.RequestSHA != result.Intent.RequestSHA ||
		!bytes.Equal(latest.Frozen.Body(), result.Frozen.Body()) ||
		latest.Root != result.Root || latest.Result.Bundle() != result.Result.Bundle() ||
		latest.Result.Root() != result.Result.Root() || archive.Bundle() != result.Result.Bundle() ||
		!sameExportRefs(latest.Result.Documents(), result.Result.Documents()) {
		return noResult, noArchive, nil, v2input.ErrInvalid
	}
	return latest, archive, origins, ctx.Err()
}

func sameExportRefs(left, right []v2input.Ref) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
