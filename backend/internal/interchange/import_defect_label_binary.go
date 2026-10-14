package interchange

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type ImportDefectLabelOccurrencePublisher interface {
	PublishImportDefectLabel(context.Context, uuid.UUID, v2input.Ref, objectstore.Object, v2input.Ref, v2input.Ref, int64, int64) (v2input.Ref, error)
}

// Copies only a label array verified against its archived proposal. The
// occurrence publisher must bind the parent and geometry in SQL before the
// immutable copy is visible to the prepared closure.
type ImportDefectLabelBinaryRemapper struct {
	Workflow  uuid.UUID
	Parsed    []byte
	Reader    ImportArchiveDocumentReader
	Qualifier ImportDefectCandidatesQualifier
	Sources   ImportRecipeSourceQualifier
	Bindings  []ImportCorrectionBinding
	Catalog   ImportDocumentCatalog
	Objects   ImportDocumentObjects
	Masters   ImportMasterReader
	Publisher ImportDefectLabelOccurrencePublisher
}

func (r ImportDefectLabelBinaryRemapper) RemapImportBinary(ctx context.Context, entry uuid.UUID, archived, staged v2input.Ref, kind string, old, target v2input.SessionVersionBinding, maxBytes int64) (v2input.Ref, int64, error) {
	zero := v2input.Ref{}
	if err := ctx.Err(); err != nil {
		return zero, 0, err
	}
	if kind != "DefectLabelMap" {
		return zero, 0, &ImportRecipeUnsupported{Reason: "binary_dependency_codec_not_installed"}
	}
	if r.Publisher == nil {
		return zero, 0, &ImportRecipeUnsupported{Reason: "defect_label_occurrence_publisher_not_installed"}
	}
	if r.Workflow == uuid.Nil || entry == uuid.Nil || r.Reader == nil || r.Qualifier == nil || r.Sources == nil || r.Catalog == nil || r.Objects == nil || r.Masters == nil || !validImportRef(archived) || !validImportRef(staged) || archived.SHA256 != staged.SHA256 || !validImportRef(old.Source) || !validImportRef(target.Source) || old.CanvasHash != target.CanvasHash || maxBytes < 1 || maxBytes > 128<<30 {
		return zero, 0, v2input.ErrInvalid
	}
	matched := false
	for _, binding := range r.Bindings {
		if binding.Entry == entry && binding.Old == old && binding.New == target {
			matched = true
			break
		}
	}
	if !matched {
		return zero, 0, v2input.ErrInvalid
	}
	qualified := BoundImportDefectCandidatesQualifier{Entry: entry, Old: old, Target: target, Parsed: append([]byte(nil), r.Parsed...), Reader: r.Reader, Sources: r.Sources, Semantic: r.Qualifier}
	source := ImportArchivedDefectLabelSource{Parsed: append([]byte(nil), r.Parsed...), Reader: r.Reader, Qualifier: qualified, Old: old}
	parent, err := source.ResolveImportDefectLabelParent(ctx, v2input.Entry{Ref: archived, Contract: "DefectLabelMap"})
	if err != nil {
		return zero, 0, err
	}
	proposals, err := source.ResolveExportDefectLabels(ctx, v2input.Entry{Ref: archived, Contract: "DefectLabelMap"})
	if err != nil || len(proposals) != 1 {
		return zero, 0, v2input.ErrInvalid
	}
	binding, err := proposals[0].LabelBinding()
	if err != nil || binding.Proposal.ID != parent.ArtifactID || binding.Proposal.SHA256 != parent.SHA256 {
		return zero, 0, v2input.ErrInvalid
	}
	master, err := r.Masters.ReadImportMaster(ctx, target.Source, 1<<20)
	if err != nil {
		return zero, 0, err
	}
	image, err := review.ParseWorkingImage(master)
	if err != nil || rawSHA(master) != target.Source.SHA256 || image.Canvas.Width != binding.Width || image.Canvas.Height != binding.Height {
		return zero, 0, v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != target.CanvasHash {
		return zero, 0, v2input.ErrInvalid
	}
	stagedNode := v2input.Entry{Ref: staged, Contract: "Artifact"}
	object, err := r.Catalog.LookupImportDocument(ctx, stagedNode)
	if err != nil {
		return zero, 0, err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != archived.SHA256 || object.Bytes < 1 || object.Bytes > maxBytes || !exportArrayMediaType(object.ContentType) {
		return zero, 0, v2input.ErrInvalid
	}
	physical := ExportDefectLabelQualifier{Objects: r.Objects, Source: source}
	if err := physical.QualifyExportBinary(ctx, v2input.Entry{Ref: archived, Contract: "DefectLabelMap"}, object); err != nil {
		return zero, 0, err
	}
	current, err := r.Catalog.LookupImportDocument(ctx, stagedNode)
	if err != nil || current != object {
		return zero, 0, objectstore.ErrIntegrity
	}
	mapped := v2input.Ref{ArtifactID: uuid.NewSHA1(r.Workflow, []byte("interchange:dependency:"+entry.String()+":DefectLabelMap:"+archived.ArtifactID.String())), SHA256: archived.SHA256}
	if mapped.ArtifactID == archived.ArtifactID {
		return zero, 0, v2input.ErrInvalid
	}
	receipt, err := r.Publisher.PublishImportDefectLabel(ctx, entry, mapped, object, archived, parent, binding.Width, binding.Height)
	if err != nil {
		return zero, 0, err
	}
	if receipt != mapped {
		return zero, 0, objectstore.ErrIntegrity
	}
	current, err = r.Catalog.LookupImportDocument(ctx, stagedNode)
	if err != nil || current != object {
		return zero, 0, objectstore.ErrIntegrity
	}
	currentMaster, err := r.Masters.ReadImportMaster(ctx, target.Source, 1<<20)
	if err != nil || !bytes.Equal(currentMaster, master) {
		return zero, 0, v2input.ErrInvalid
	}
	return mapped, object.Bytes, ctx.Err()
}

var _ ImportBinaryRemapper = ImportDefectLabelBinaryRemapper{}
