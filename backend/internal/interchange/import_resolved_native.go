package interchange

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Scope is separate from the schema probe: it verifies that every model,
// style, stats and nested override is authorized for the entry's source.
type ImportResolvedScopeAuthority interface {
	QualifyArchivedResolvedScope(context.Context, []byte, v2input.SessionVersionBinding, []v2input.Entry) error
	QualifyTargetResolvedScope(context.Context, []byte, v2input.SessionVersionBinding, []v2input.Entry) error
}

type NativeImportResolvedQualifier struct {
	native *NativeExportStyleValidator
	scope  ImportResolvedScopeAuthority
}

type importResolvedNativeAuthority struct {
	scope    ImportResolvedScopeAuthority
	binding  v2input.SessionVersionBinding
	archived bool
}

func (a importResolvedNativeAuthority) ExportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
	if a.scope == nil || entry.Contract != "ResolvedStyle" || rawSHA(raw) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	var document struct {
		Params struct {
			Image v2input.Ref `json:"image"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Params.Image != a.binding.Source {
		return nil, v2input.ErrInvalid
	}
	children, err := exportResolvedChildren(entry, raw)
	if err != nil {
		return nil, err
	}
	if a.archived {
		err = a.scope.QualifyArchivedResolvedScope(ctx, append([]byte(nil), raw...), a.binding, append([]v2input.Entry(nil), children...))
	} else {
		err = a.scope.QualifyTargetResolvedScope(ctx, append([]byte(nil), raw...), a.binding, append([]v2input.Entry(nil), children...))
	}
	if err != nil {
		return nil, err
	}
	return children, ctx.Err()
}

func NewNativeImportResolvedQualifier(scratch *ExportArchiveScratch, probe ExportNativeProbe, models map[string]string, runtime string, scope ImportResolvedScopeAuthority) (*NativeImportResolvedQualifier, error) {
	if scope == nil {
		return nil, v2input.ErrInvalid
	}
	native, err := NewNativeExportResolvedValidator(scratch, probe, models, runtime, importResolvedNativeAuthority{scope: scope})
	if err != nil {
		return nil, err
	}
	return &NativeImportResolvedQualifier{native: native, scope: scope}, nil
}

func (q *NativeImportResolvedQualifier) ValidateArchivedImportResolved(ctx context.Context, raw []byte, binding v2input.SessionVersionBinding) error {
	return q.qualify(ctx, raw, binding, true)
}

func (q *NativeImportResolvedQualifier) QualifyImportedResolved(ctx context.Context, raw []byte, binding v2input.SessionVersionBinding) error {
	return q.qualify(ctx, raw, binding, false)
}

func (q *NativeImportResolvedQualifier) qualify(ctx context.Context, raw []byte, binding v2input.SessionVersionBinding, archived bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q == nil || q.native == nil || q.scope == nil || binding.AssetVersionID == uuid.Nil || !validImportRef(binding.Source) || !validImportRef(v2input.Ref{ArtifactID: binding.AssetVersionID, SHA256: binding.CanvasHash}) || len(raw) < 2 || len(raw) > 8<<20 {
		return v2input.ErrInvalid
	}
	sha := rawSHA(raw)
	entry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("interchange:validate-resolved:"+sha)), SHA256: sha}, Contract: "ResolvedStyle"}
	native := *q.native
	native.authority = importResolvedNativeAuthority{scope: q.scope, binding: binding, archived: archived}
	_, err := native.ExportDocumentChildren(ctx, entry, append([]byte(nil), raw...))
	return err
}

var _ ImportResolvedQualifier = (*NativeImportResolvedQualifier)(nil)
