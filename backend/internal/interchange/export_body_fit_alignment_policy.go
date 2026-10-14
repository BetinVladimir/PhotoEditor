package interchange

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type ExportAlignmentQualificationReader interface {
	ReadExportBodyFitAlignmentQualification(context.Context, ImportDocumentObjects, v2input.Ref, v2qualification.Expected) (ExportBodyDependencyQualification, error)
	MatchExportBodyFitAlignmentProfile(context.Context, ExportBodyDependencyOrigin, v2input.Entry, []byte, ExportClosureNode, v2input.Entry, v2qualification.Expected) error
}

// Must bind original before/after sources and qualify any original background
// mask, including its physical content and current causal/model authority.
type ExportAlignmentOriginalInputAuthority interface {
	QualifyExportAlignmentOriginalInputs(context.Context, ExportBodyDependencyOrigin, v2input.Entry, []byte, ExportClosureNode, v2input.Entry, []byte) error
}
type ExportAlignmentPolicy struct {
	reader   ExportAlignmentQualificationReader
	objects  ImportDocumentObjects
	inputs   ExportAlignmentOriginalInputAuthority
	report   v2input.Ref
	expected []byte
}

func NewExportAlignmentPolicy(reader ExportAlignmentQualificationReader, objects ImportDocumentObjects, inputs ExportAlignmentOriginalInputAuthority, report v2input.Ref, expected v2qualification.Expected) (*ExportAlignmentPolicy, error) {
	if reader == nil || objects == nil || inputs == nil || !validImportRef(report) || !validExportAlignmentProfile(expected) {
		return nil, v2input.ErrInvalid
	}
	frozen, err := canonical.Marshal(expected)
	if err != nil {
		return nil, err
	}
	return &ExportAlignmentPolicy{reader: reader, objects: objects, inputs: inputs, report: report, expected: frozen}, nil
}
func (p *ExportAlignmentPolicy) QualifyExportBodyFitAlignmentProfile(ctx context.Context, origin ExportBodyDependencyOrigin, parent v2input.Entry, raw []byte, fit ExportClosureNode, alignment v2input.Entry, params []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.reader == nil || p.objects == nil || p.inputs == nil || origin.Producer.Operation != "pair.register" || origin.Producer.Version != "1" || parent.Contract != "Params" || !validImportRef(parent.Ref) || len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != parent.Ref.SHA256 || len(params) < 2 || len(params) > 1<<20 || rawSHA(params) != origin.Params.SHA256 {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	original := append([]byte(nil), params...)
	owned := ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fit.Body...)}
	if _, err := exportBodyFitAlignmentEdge(owned, alignment); err != nil {
		return err
	}
	clone := func() ExportClosureNode {
		return ExportClosureNode{Entry: owned.Entry, Body: append([]byte(nil), owned.Body...)}
	}
	fresh := func() (v2qualification.Expected, error) {
		var expected v2qualification.Expected
		err := json.Unmarshal(p.expected, &expected)
		return expected, err
	}
	check := func() error {
		expected, err := fresh()
		if err != nil {
			return err
		}
		if err := p.reader.MatchExportBodyFitAlignmentProfile(ctx, origin, parent, append([]byte(nil), body...), clone(), alignment, expected); err != nil {
			return err
		}
		return p.inputs.QualifyExportAlignmentOriginalInputs(ctx, origin, parent, append([]byte(nil), body...), clone(), alignment, append([]byte(nil), original...))
	}
	expected, err := fresh()
	if err != nil {
		return err
	}
	before, err := p.reader.ReadExportBodyFitAlignmentQualification(ctx, p.objects, p.report, expected)
	if err != nil {
		return err
	}
	if before.Report != p.report {
		return v2input.ErrInvalid
	}
	if err := check(); err != nil {
		return err
	}
	expected, err = fresh()
	if err != nil {
		return err
	}
	after, err := p.reader.ReadExportBodyFitAlignmentQualification(ctx, p.objects, p.report, expected)
	if err != nil {
		return err
	}
	if after != before {
		return v2input.ErrInvalid
	}
	if err := check(); err != nil {
		return err
	}
	return ctx.Err()
}

var _ ExportAlignmentQualificationReader = SQLExportDocumentCatalog{}
var _ ExportAlignmentQualificationReader = SQLExportResultInputCatalog{}
var _ ExportBodyFitAlignmentPolicy = (*ExportAlignmentPolicy)(nil)
