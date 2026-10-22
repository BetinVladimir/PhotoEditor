package interchange

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// A codec validates the complete old typed document, rewrites its typed refs
// through resolve, and rewrites source/UUID fields according to that contract.
// It must preserve supported authored values and reject unknown semantics.
// Binary arrays require a separate physical codec and must never enter here.
type ImportDependencyCodec interface {
	RemapImportDocument(context.Context, string, []byte, v2input.SessionVersionBinding, v2input.SessionVersionBinding, func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error)
	QualifyRemappedImportDocument(context.Context, string, []byte, v2input.SessionVersionBinding) error
}
type ImportDocumentSourceBinder interface {
	BindImportDocumentSource(v2input.Ref) ImportDependencyCodec
}
type ImportDependencyPublisher interface {
	PublishImportDependency(context.Context, uuid.UUID, string, v2input.Ref, []byte) (v2input.Ref, error)
}
type RecursiveImportDependencyRemapper struct {
	workflow  uuid.UUID
	archive   map[uuid.UUID]importArchivedSource
	Reader    ImportArchiveDocumentReader
	Sources   ImportRecipeSourceQualifier
	Codec     ImportDependencyCodec
	Publisher ImportDependencyPublisher
	Binary    ImportBinaryRemapper
}

func NewRecursiveImportDependencyRemapper(workflow uuid.UUID, parsedRaw []byte, reader ImportArchiveDocumentReader, sources ImportRecipeSourceQualifier, codec ImportDependencyCodec, publisher ImportDependencyPublisher) (*RecursiveImportDependencyRemapper, error) {
	if workflow == uuid.Nil || reader == nil || sources == nil || codec == nil || publisher == nil {
		return nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(context.Background(), parsedRaw); err != nil {
		return nil, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(parsedRaw, &parsed); err != nil {
		return nil, err
	}
	files := map[string]v2input.Ref{}
	for _, file := range parsed.Files {
		files[file.Path] = file.Content
	}
	archive := map[uuid.UUID]importArchivedSource{}
	for _, item := range parsed.Manifest.Inventory {
		if _, ok := archive[item.ID]; ok {
			return nil, v2input.ErrInvalid
		}
		archive[item.ID] = importArchivedSource{staged: files[item.Path], sha: item.SHA, contract: item.Contract}
	}
	return &RecursiveImportDependencyRemapper{workflow: workflow, archive: archive, Reader: reader, Sources: sources, Codec: codec, Publisher: publisher}, nil
}
func (r *RecursiveImportDependencyRemapper) RemapImportRecipeDependency(ctx context.Context, entry uuid.UUID, root v2input.Ref, contract string, old, target v2input.SessionVersionBinding) (v2input.Ref, error) {
	return r.remapImportRecipeDependency(ctx, entry, root, contract, old, target, 128<<20)
}

// The public path always uses the full 128 MiB JSON envelope. The same
// traversal can use a smaller internal envelope for bounded boundary scenarios.
func (r *RecursiveImportDependencyRemapper) remapImportRecipeDependency(ctx context.Context, entry uuid.UUID, root v2input.Ref, contract string, old, target v2input.SessionVersionBinding, jsonBudget int64) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if jsonBudget < 2 || jsonBudget > 128<<20 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if r == nil || r.workflow == uuid.Nil || entry == uuid.Nil || r.Reader == nil || r.Sources == nil || r.Codec == nil || r.Publisher == nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if err := r.Sources.QualifyImportRecipeSource(ctx, entry, old, target); err != nil {
		return v2input.Ref{}, err
	}
	// State belongs to one exact entry/source invocation, never shared across
	// tenant contexts or exposed as a mutable cache on the installed remapper.
	active := map[uuid.UUID]bool{}
	done := map[uuid.UUID]v2input.Ref{}
	types := map[uuid.UUID]string{}
	var total, preparedTotal, binaryTotal int64
	var visit func(v2input.Ref, string, int) (v2input.Ref, error)
	visit = func(ref v2input.Ref, kind string, depth int) (v2input.Ref, error) {
		if err := ctx.Err(); err != nil {
			return v2input.Ref{}, err
		}
		if !validImportRef(ref) || len(kind) < 1 || len(kind) > 120 || depth > 32 {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		if kind == "WorkingImage" && ref == old.Source {
			return target.Source, nil
		}
		source, ok := r.archive[ref.ArtifactID]
		if !ok || source.sha != ref.SHA256 || source.contract != kind {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		if previous, ok := types[ref.ArtifactID]; ok && previous != kind {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		if active[ref.ArtifactID] {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		if mapped, ok := done[ref.ArtifactID]; ok {
			return mapped, nil
		}
		if len(types) >= 30000 {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		types[ref.ArtifactID] = kind
		if kind == "MaskPixels" || kind == "DenseWarpField" || kind == "WorkingPixels" || kind == "WorkingAlpha" || kind == "NpyArray" || kind == "AbsoluteLUTArray" || kind == "DefectLabelMap" || kind == "DefectLabelPNG" {
			if r.Binary == nil {
				return v2input.Ref{}, &ImportRecipeUnsupported{Reason: "binary_dependency_codec_not_installed"}
			}
			remaining := (int64(128) << 30) - binaryTotal
			if remaining < 1 {
				return v2input.Ref{}, v2input.ErrInvalid
			}
			mapped, size, err := r.Binary.RemapImportBinary(ctx, entry, ref, source.staged, kind, old, target, remaining)
			if err != nil {
				return v2input.Ref{}, err
			}
			expected := uuid.NewSHA1(r.workflow, []byte("interchange:dependency:"+entry.String()+":"+kind+":"+ref.ArtifactID.String()))
			if !validImportRef(mapped) || mapped.ArtifactID != expected || mapped.SHA256 != ref.SHA256 || size < 1 || size > remaining {
				return v2input.Ref{}, v2input.ErrInvalid
			}
			if err := ctx.Err(); err != nil {
				return v2input.Ref{}, err
			}
			binaryTotal += size
			done[ref.ArtifactID] = mapped
			return mapped, nil
		}
		remaining := jsonBudget - total
		if remaining < 2 {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		limit := int64(8) << 20
		if remaining < limit {
			limit = remaining
		}
		raw, err := r.Reader.ReadImportArchiveDocument(ctx, source.staged, limit)
		if err != nil {
			return v2input.Ref{}, err
		}
		raw = append([]byte(nil), raw...)
		if int64(len(raw)) < 2 || int64(len(raw)) > limit || rawSHA(raw) != ref.SHA256 {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		total += int64(len(raw))
		normalized, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(normalized, raw) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		active[ref.ArtifactID] = true
		codec := r.Codec
		if binder, ok := codec.(ImportDocumentSourceBinder); ok {
			codec = binder.BindImportDocumentSource(ref)
			if codec == nil {
				return v2input.Ref{}, v2input.ErrInvalid
			}
		}
		codecCtx := withImportConfirmationScope(ctx, r.workflow, entry, ref, kind)
		prepared, err := codec.RemapImportDocument(codecCtx, kind, raw, old, target, func(child v2input.Ref, childKind string) (v2input.Ref, error) {
			return visit(child, childKind, depth+1)
		})
		delete(active, ref.ArtifactID)
		if err != nil {
			return v2input.Ref{}, err
		}
		// A codec can expand small archive JSON. Bound the aggregate prepared
		// bytes before copying/qualification/publication, not merely each document.
		size := int64(len(prepared))
		if size < 2 || size > 8<<20 || size > jsonBudget-preparedTotal {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		prepared = append([]byte(nil), prepared...)
		normalized, err = canonical.Bytes(prepared)
		if err != nil || !bytes.Equal(normalized, prepared) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		if err := codec.QualifyRemappedImportDocument(codecCtx, kind, append([]byte(nil), prepared...), target); err != nil {
			return v2input.Ref{}, err
		}
		if err := ctx.Err(); err != nil {
			return v2input.Ref{}, err
		}
		mapped := v2input.Ref{ArtifactID: uuid.NewSHA1(r.workflow, []byte("interchange:dependency:"+entry.String()+":"+kind+":"+ref.ArtifactID.String())), SHA256: rawSHA(prepared)}
		if mapped.ArtifactID == ref.ArtifactID {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		// Archive/native/codec IO may outlive the source authorization observed
		// at entry. Recheck before creating a prepared JSON publication.
		if err := r.Sources.QualifyImportRecipeSource(ctx, entry, old, target); err != nil {
			return v2input.Ref{}, err
		}
		if err := ctx.Err(); err != nil {
			return v2input.Ref{}, err
		}
		receipt, err := r.Publisher.PublishImportDependency(ctx, entry, kind, mapped, append([]byte(nil), prepared...))
		if err != nil {
			return v2input.Ref{}, err
		}
		if err := ctx.Err(); err != nil {
			return v2input.Ref{}, err
		}
		if receipt != mapped {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		preparedTotal += size
		done[ref.ArtifactID] = mapped
		return mapped, nil
	}
	mapped, err := visit(root, contract, 0)
	if err != nil {
		return v2input.Ref{}, err
	}
	// Publication can commit before a revocation. Its receipt is not current
	// source authority and must not escape as an approved remap result.
	if err := r.Sources.QualifyImportRecipeSource(ctx, entry, old, target); err != nil {
		return v2input.Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	return mapped, nil
}
