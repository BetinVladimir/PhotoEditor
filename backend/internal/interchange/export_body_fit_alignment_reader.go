package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type ExportBodyFitAlignmentOriginSource interface {
	LookupExportBodyFitAlignmentOrigin(context.Context, v2input.Entry, []byte, ExportClosureNode, v2input.Entry) (ExportBodyDependencyOrigin, error)
}
type ExportBodyFitAlignmentParamsReader struct {
	Source  ExportBodyFitAlignmentOriginSource
	Objects ImportDocumentObjects
}

// Reads the original producer Params, never inferred Params from its report.
// Production Params/content/model qualification is the caller's next gate.
func (r ExportBodyFitAlignmentParamsReader) ReadExportBodyFitAlignmentParams(ctx context.Context, parent v2input.Entry, raw []byte, fit ExportClosureNode, child v2input.Entry) (ExportBodyDependencyOrigin, []byte, error) {
	var zero ExportBodyDependencyOrigin
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	if r.Source == nil || r.Objects == nil {
		return zero, nil, v2input.ErrInvalid
	}
	if parent.Contract != "Params" || !validImportRef(parent.Ref) || len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != parent.Ref.SHA256 {
		return zero, nil, v2input.ErrInvalid
	}
	fit = ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fit.Body...)}
	if _, err := exportBodyFitAlignmentEdge(fit, child); err != nil {
		return zero, nil, err
	}
	clone := func() ExportClosureNode {
		return ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fit.Body...)}
	}
	body := append([]byte(nil), raw...)
	origin, err := r.Source.LookupExportBodyFitAlignmentOrigin(ctx, parent, append([]byte(nil), body...), clone(), child)
	if err != nil {
		return zero, nil, err
	}
	if origin.Producer.Task == uuid.Nil || origin.Producer.Operation != "pair.register" || origin.Producer.Version != "1" || origin.Producer.Attempt < 1 || origin.Producer.Fence < 1 || !validImportRef(origin.Params) || origin.Params.ArtifactID == child.Ref.ArtifactID || origin.Params.ArtifactID == parent.Ref.ArtifactID || origin.Params.ArtifactID == fit.Entry.Ref.ArtifactID || origin.Output.SHA256 != child.Ref.SHA256 || origin.Output.ContentType != "application/json" || origin.Output.Bytes < 2 || origin.Output.Bytes > 2<<20 || origin.Output.Key == "" || origin.Output.VersionID == "" || origin.Output.VersionID == "null" {
		return zero, nil, v2input.ErrInvalid
	}
	params, err := readExportRecipeObject(ctx, r.Objects, origin.Params, origin.ParamsObject)
	if err != nil {
		return zero, nil, err
	}
	after, err := r.Source.LookupExportBodyFitAlignmentOrigin(ctx, parent, append([]byte(nil), body...), clone(), child)
	if err != nil {
		return zero, nil, err
	}
	if after != origin {
		return zero, nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	return origin, params, nil
}

var _ ExportBodyFitAlignmentOriginSource = SQLExportDocumentCatalog{}
var _ ExportBodyFitAlignmentOriginSource = SQLExportResultInputCatalog{}
