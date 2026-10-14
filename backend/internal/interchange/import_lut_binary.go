package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// ImportLUTBinaryRemapper verifies the original 4D float32 NPY through the
// pinned native array probe and a qualified ResolvedStyle parent before copy.
type ImportLUTBinaryRemapper struct {
	Workflow   uuid.UUID
	Parsed     []byte
	Reader     ImportArchiveDocumentReader
	Resolved   ImportResolvedQualifier
	Catalog    ImportDocumentCatalog
	Objects    ImportDocumentObjects
	Scratch    *ExportArchiveScratch
	Probe      ExportNativeProbe
	RuntimeSHA string
	Publisher  ImportBinaryPublisher
}
type ImportLUTOccurrencePublisher interface {
	PublishImportLUT(context.Context, uuid.UUID, v2input.Ref, objectstore.Object, v2input.Ref, v2input.Ref, int) (v2input.Ref, error)
}

func (r ImportLUTBinaryRemapper) RemapImportBinary(ctx context.Context, entry uuid.UUID, archived, staged v2input.Ref, kind string, old, target v2input.SessionVersionBinding, maxBytes int64) (v2input.Ref, int64, error) {
	var empty v2input.Ref
	if err := ctx.Err(); err != nil {
		return empty, 0, err
	}
	if kind != "AbsoluteLUTArray" {
		return empty, 0, &ImportRecipeUnsupported{Reason: "binary_dependency_codec_not_installed"}
	}
	if r.Workflow == uuid.Nil || entry == uuid.Nil || r.Reader == nil || r.Resolved == nil || r.Catalog == nil || r.Objects == nil || r.Scratch == nil || r.Probe == nil || r.Publisher == nil || !validImportRefForSHA(r.RuntimeSHA) || !validImportRef(archived) || !validImportRef(staged) || archived.SHA256 != staged.SHA256 || old.CanvasHash != target.CanvasHash || maxBytes < 1 || maxBytes > 128<<30 {
		return empty, 0, v2input.ErrInvalid
	}
	geometry := ImportArchivedLUTGeometrySource{Parsed: append([]byte(nil), r.Parsed...), Reader: r.Reader, Qualifier: r.Resolved, Old: old}
	original := v2input.Entry{Ref: archived, Contract: "AbsoluteLUTArray"}
	shape, err := geometry.ResolveExportArrayGeometry(ctx, original)
	if err != nil {
		return empty, 0, err
	}
	parent, err := geometry.ResolveImportLUTParent(ctx, original)
	if err != nil {
		return empty, 0, err
	}
	stagedNode := v2input.Entry{Ref: staged, Contract: "Artifact"}
	object, err := r.Catalog.LookupImportDocument(ctx, stagedNode)
	if err != nil {
		return empty, 0, err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != archived.SHA256 || object.Bytes < 1 || object.Bytes > maxBytes || !exportArrayMediaType(object.ContentType) {
		return empty, 0, v2input.ErrInvalid
	}
	native := NativeExportArrayQualifier{Objects: r.Objects, Scratch: r.Scratch, Probe: r.Probe, Geometry: geometry, RuntimeSHA: r.RuntimeSHA}
	if err := native.QualifyExportBinary(ctx, original, object); err != nil {
		return empty, 0, err
	}
	current, err := r.Catalog.LookupImportDocument(ctx, stagedNode)
	if err != nil {
		return empty, 0, err
	}
	if current != object {
		return empty, 0, objectstore.ErrIntegrity
	}
	mapped := v2input.Ref{ArtifactID: uuid.NewSHA1(r.Workflow, []byte("interchange:dependency:"+entry.String()+":"+kind+":"+archived.ArtifactID.String())), SHA256: archived.SHA256}
	if mapped.ArtifactID == archived.ArtifactID {
		return empty, 0, v2input.ErrInvalid
	}
	occurrencePublisher, ok := r.Publisher.(ImportLUTOccurrencePublisher)
	if !ok {
		return empty, 0, &ImportRecipeUnsupported{Reason: "lut_occurrence_publisher_not_installed"}
	}
	receipt, err := occurrencePublisher.PublishImportLUT(ctx, entry, mapped, object, archived, parent, shape.Width)
	if err != nil {
		return empty, 0, err
	}
	if receipt != mapped {
		return empty, 0, objectstore.ErrIntegrity
	}
	current, err = r.Catalog.LookupImportDocument(ctx, stagedNode)
	if err != nil {
		return empty, 0, err
	}
	if current != object {
		return empty, 0, objectstore.ErrIntegrity
	}
	if _, err := geometry.ResolveExportArrayGeometry(ctx, original); err != nil {
		return empty, 0, err
	}
	return mapped, object.Bytes, ctx.Err()
}

var _ ImportBinaryRemapper = ImportLUTBinaryRemapper{}
