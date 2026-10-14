package interchange

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type ExportBodyFitFeatureQualificationReader interface {
	ReadExportBodyDependencyQualification(context.Context, ImportDocumentObjects, v2input.Ref, v2qualification.Expected) (ExportBodyDependencyQualification, error)
	MatchExportBodyFitFeatureProfile(context.Context, ExportBodyDependencyOrigin, v2input.Entry, []byte, ExportClosureNode, v2input.Entry, v2qualification.Expected) error
}

// Must bind original side image and, for landmarks, qualify the original
// FaceBoxes/detector chain under its real producer and current model policy.
type ExportBodyFitFeatureOriginalAuthority interface {
	QualifyExportBodyFitFeatureOriginal(context.Context, ExportBodyDependencyOrigin, v2input.Entry, []byte, ExportClosureNode, v2input.Entry, []byte) error
}
type ExportBodyFitFeaturePolicy struct {
	reader   ExportBodyFitFeatureQualificationReader
	objects  ImportDocumentObjects
	inputs   ExportBodyFitFeatureOriginalAuthority
	policies map[string]exportBodyDependencyFrozenPolicy
}

func NewExportBodyFitFeaturePolicy(reader ExportBodyFitFeatureQualificationReader, objects ImportDocumentObjects, inputs ExportBodyFitFeatureOriginalAuthority, configs []ExportBodyDependencyPolicyConfig) (*ExportBodyFitFeaturePolicy, error) {
	if reader == nil || objects == nil || inputs == nil || len(configs) != 2 {
		return nil, v2input.ErrInvalid
	}
	result := &ExportBodyFitFeaturePolicy{reader: reader, objects: objects, inputs: inputs, policies: map[string]exportBodyDependencyFrozenPolicy{}}
	reports := map[[16]byte]bool{}
	for _, config := range configs {
		e := config.Expected
		if (e.Operation != "pose.detect" && e.Operation != "face.landmarks") || e.Version != "1" || !e.LicenseCleared || !validImportRef(config.Report) || reports[config.Report.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		if e.Backend != "cpu" && e.Backend != "mps" && e.Backend != "cuda" {
			return nil, v2input.ErrInvalid
		}
		if _, exists := result.policies[e.Operation]; exists {
			return nil, v2input.ErrInvalid
		}
		for _, sha := range []string{e.ModelLockSHA, e.RuntimeLockSHA, e.DeviceProfileSHA, e.DatasetSHA} {
			if !validImportRef(v2input.Ref{ArtifactID: config.Report.ArtifactID, SHA256: sha}) {
				return nil, v2input.ErrInvalid
			}
		}
		frozen, err := canonical.Marshal(e)
		if err != nil {
			return nil, err
		}
		reports[config.Report.ArtifactID] = true
		result.policies[e.Operation] = exportBodyDependencyFrozenPolicy{report: config.Report, expected: frozen}
	}
	return result, nil
}
func (p *ExportBodyFitFeaturePolicy) QualifyExportBodyFitFeatureProfile(ctx context.Context, origin ExportBodyDependencyOrigin, parent v2input.Entry, raw []byte, fit ExportClosureNode, child v2input.Entry, params []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.reader == nil || p.objects == nil || p.inputs == nil || parent.Contract != "Params" || !validImportRef(parent.Ref) || len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != parent.Ref.SHA256 || len(params) < 2 || len(params) > 1<<20 || rawSHA(params) != origin.Params.SHA256 {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	original := append([]byte(nil), params...)
	fit = ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fit.Body...)}
	edge, err := exportBodyFitFeatureEdge(fit, child)
	if err != nil {
		return err
	}
	if edge.Operation != origin.Producer.Operation || origin.Producer.Version != "1" {
		return v2input.ErrInvalid
	}
	configured, ok := p.policies[edge.Operation]
	if !ok {
		return v2qualification.ErrUnqualified
	}
	clone := func() ExportClosureNode {
		return ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fit.Body...)}
	}
	fresh := func() (v2qualification.Expected, error) {
		var e v2qualification.Expected
		err := json.Unmarshal(configured.expected, &e)
		return e, err
	}
	check := func() error {
		e, err := fresh()
		if err != nil {
			return err
		}
		if err := p.reader.MatchExportBodyFitFeatureProfile(ctx, origin, parent, append([]byte(nil), body...), clone(), child, e); err != nil {
			return err
		}
		return p.inputs.QualifyExportBodyFitFeatureOriginal(ctx, origin, parent, append([]byte(nil), body...), clone(), child, append([]byte(nil), original...))
	}
	e, err := fresh()
	if err != nil {
		return err
	}
	before, err := p.reader.ReadExportBodyDependencyQualification(ctx, p.objects, configured.report, e)
	if err != nil {
		return err
	}
	if before.Report != configured.report {
		return v2input.ErrInvalid
	}
	if err := check(); err != nil {
		return err
	}
	e, err = fresh()
	if err != nil {
		return err
	}
	after, err := p.reader.ReadExportBodyDependencyQualification(ctx, p.objects, configured.report, e)
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

var _ ExportBodyFitFeatureQualificationReader = SQLExportDocumentCatalog{}
var _ ExportBodyFitFeatureQualificationReader = SQLExportResultInputCatalog{}
var _ ExportBodyFitFeatureProfileAuthority = (*ExportBodyFitFeaturePolicy)(nil)
