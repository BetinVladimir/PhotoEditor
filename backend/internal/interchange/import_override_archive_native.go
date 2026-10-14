package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// This preflight checks the archived Override schema and operator model pins
// before a background publisher can create any target-side bytes. Source,
// portable asset, mask and merged-style authority are checked separately.
type NativeImportOverrideArchiveQualifier struct {
	native *NativeExportStyleValidator
}

func NewNativeImportOverrideArchiveQualifier(scratch *ExportArchiveScratch, probe ExportNativeProbe, models map[string]string, runtime string) (*NativeImportOverrideArchiveQualifier, error) {
	native, err := NewNativeExportOverrideValidator(scratch, probe, models, runtime, exportCompositionStagingValidator{})
	if err != nil {
		return nil, err
	}
	return &NativeImportOverrideArchiveQualifier{native: native}, nil
}

func (q *NativeImportOverrideArchiveQualifier) ValidateArchivedImportOverride(ctx context.Context, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q == nil || q.native == nil || len(raw) < 2 || len(raw) > 8<<20 {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	sha := rawSHA(body)
	entry := v2input.Entry{Contract: "Override", Ref: v2input.Ref{
		ArtifactID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("interchange:archived-override:"+sha)), SHA256: sha,
	}}
	_, err := q.native.ExportDocumentChildren(ctx, entry, body)
	return err
}

var _ ImportOverrideArchiveQualifier = (*NativeImportOverrideArchiveQualifier)(nil)
