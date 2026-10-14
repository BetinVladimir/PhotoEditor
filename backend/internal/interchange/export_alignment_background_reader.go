package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type ExportAlignmentBackgroundOriginSource interface {
	LookupExportAlignmentBackgroundOrigin(context.Context, ExportBodyDependencyOrigin, v2input.Entry, []byte, ExportClosureNode, v2input.Entry, []byte, ExportClosureNode) (ExportSeriesMaskOrigin, error)
}
type ExportAlignmentBackgroundOriginReader struct {
	Source  ExportAlignmentBackgroundOriginSource
	Objects ImportDocumentObjects
}

func (r ExportAlignmentBackgroundOriginReader) ReadExportAlignmentBackgroundOrigin(ctx context.Context, registration ExportBodyDependencyOrigin, parent v2input.Entry, raw []byte, fit ExportClosureNode, alignment v2input.Entry, params []byte, image ExportClosureNode) (ExportSeriesMaskOrigin, []byte, error) {
	var zero ExportSeriesMaskOrigin
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	if r.Source == nil || r.Objects == nil {
		return zero, nil, v2input.ErrInvalid
	}
	mask, err := exportAlignmentBackgroundProjection(registration, parent, raw, fit, alignment, params, image)
	if err != nil {
		return zero, nil, err
	}
	ref := v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
	original := append([]byte(nil), params...)
	fitBody := append([]byte(nil), fit.Body...)
	children := append([]v2input.Entry(nil), image.Children...)
	body := append([]byte(nil), raw...)
	imageBody := append([]byte(nil), image.Body...)
	readOrigin := func() (ExportSeriesMaskOrigin, error) {
		return r.Source.LookupExportAlignmentBackgroundOrigin(ctx, registration, parent, append([]byte(nil), body...), ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fitBody...)}, alignment, append([]byte(nil), original...), ExportClosureNode{Entry: image.Entry, Body: append([]byte(nil), imageBody...), Children: append([]v2input.Entry(nil), children...)})
	}
	origin, err := readOrigin()
	if err != nil {
		return zero, nil, err
	}
	if origin.Task == uuid.Nil || origin.Attempt < 1 || origin.Fence < 1 || origin.Version != "1" || (origin.Operation != "mask.person" && origin.Operation != "mask.prompt") || origin.Mask.SHA256 != ref.SHA256 {
		return zero, nil, v2input.ErrInvalid
	}
	documents := []ExportSeriesMaskDocument{origin.Root, origin.Params, origin.Report}
	seen := map[uuid.UUID]bool{ref.ArtifactID: true}
	for _, doc := range documents {
		object := doc.Object
		if !validImportRef(doc.Ref) || seen[doc.Ref.ArtifactID] || object.SHA256 != doc.Ref.SHA256 || object.ContentType != "application/json" || object.Bytes < 2 || object.Bytes > 1<<20 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" {
			return zero, nil, v2input.ErrInvalid
		}
		seen[doc.Ref.ArtifactID] = true
	}
	pins := make([]exportCompositionDocument, 0, 3)
	for _, doc := range documents {
		stream, err := r.Objects.Open(ctx, doc.Object)
		if err != nil {
			if stream != nil {
				_ = stream.Close()
			}
			return zero, nil, err
		}
		if stream == nil {
			return zero, nil, v2input.ErrInvalid
		}
		data, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx, stream}, doc.Object.Bytes+1))
		closeErr := stream.Close()
		if readErr != nil {
			return zero, nil, readErr
		}
		if closeErr != nil {
			return zero, nil, closeErr
		}
		if int64(len(data)) != doc.Object.Bytes || rawSHA(data) != doc.Ref.SHA256 {
			return zero, nil, v2input.ErrInvalid
		}
		pins = append(pins, exportCompositionDocument{SHA: doc.Ref.SHA256, Body: data})
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(original, &fields) != nil {
		return zero, nil, v2input.ErrInvalid
	}
	inline, err := canonical.Bytes(fields["background_mask"])
	if err != nil || !bytes.Equal(inline, pins[0].Body) {
		return zero, nil, v2input.ErrInvalid
	}
	current, err := readOrigin()
	if err != nil {
		return zero, nil, err
	}
	if current != origin {
		return zero, nil, v2input.ErrInvalid
	}
	bundle, err := canonical.Marshal(struct {
		Schema    int                       `json:"schema_version"`
		Operation string                    `json:"operation"`
		Version   string                    `json:"version"`
		Root      exportCompositionDocument `json:"root"`
		Params    exportCompositionDocument `json:"params"`
		Report    exportCompositionDocument `json:"report"`
		Image     exportCompositionDocument `json:"image"`
	}{1, origin.Operation, origin.Version, pins[0], pins[1], pins[2], exportCompositionDocument{SHA: image.Entry.Ref.SHA256, Body: imageBody}})
	if err != nil {
		return zero, nil, err
	}
	if len(bundle) > 12<<20 {
		return zero, nil, v2input.ErrInvalid
	}
	return origin, bundle, ctx.Err()
}

var _ ExportAlignmentBackgroundOriginSource = SQLExportDocumentCatalog{}
var _ ExportAlignmentBackgroundOriginSource = SQLExportResultInputCatalog{}
