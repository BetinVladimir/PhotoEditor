package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type ExportBodyFitDetectorPolicyReader interface {
	ReadExportBodyFitDetectorQualification(context.Context, ImportDocumentObjects, v2input.Ref, v2qualification.Expected) (ExportBodyDependencyQualification, error)
	MatchExportBodyFitDetectorProfile(context.Context, ExportBodyDependencyOrigin, ExportBodyDependencyOrigin, v2input.Entry, []byte, ExportClosureNode, v2input.Entry, []byte, v2input.Entry, v2qualification.Expected) error
}
type QualifiedExportBodyFitFaceBoxes struct {
	reader            ExportBodyFitDetectorParamsReader
	policy            ExportBodyFitDetectorPolicyReader
	documents, images ExportClosureNodeResolver
	warps             ExportDenseWarpOccurrenceAuthority
	report            v2input.Ref
	expected          []byte
	native            *NativeExportStyleValidator
}

func NewQualifiedExportBodyFitFaceBoxes(reader ExportBodyFitDetectorParamsReader, policy ExportBodyFitDetectorPolicyReader, documents, images ExportClosureNodeResolver, warps ExportDenseWarpOccurrenceAuthority, report v2input.Ref, expected v2qualification.Expected, scratch *ExportArchiveScratch, probe ExportNativeProbe, runtime string) (*QualifiedExportBodyFitFaceBoxes, error) {
	if reader.Source == nil || reader.Objects == nil || policy == nil || documents == nil || images == nil || !validImportRef(report) || !validExportDetectorProfile(expected) {
		return nil, v2input.ErrInvalid
	}
	frozen, err := canonical.Marshal(expected)
	if err != nil {
		return nil, err
	}
	native, err := NewNativeExportStyleValidator(scratch, probe, map[string]string{"sfd": expected.ModelLockSHA}, runtime, exportCompositionStagingValidator{})
	if err != nil {
		return nil, err
	}
	native.contract = "FaceDetectorOrigin"
	return &QualifiedExportBodyFitFaceBoxes{reader: reader, policy: policy, documents: documents, images: images, warps: warps, report: report, expected: frozen, native: native}, nil
}
func (q *QualifiedExportBodyFitFaceBoxes) QualifyExportBodyFitFaceBoxes(ctx context.Context, landmarkOrigin ExportBodyDependencyOrigin, parent v2input.Entry, raw []byte, fit ExportClosureNode, landmark v2input.Entry, params []byte, image ExportClosureNode, boxes v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q == nil || q.native == nil || q.policy == nil || q.documents == nil || q.images == nil {
		return v2input.ErrInvalid
	}
	edge, err := exportBodyFitDetectorEdge(landmarkOrigin, fit, landmark, params, boxes)
	if err != nil {
		return err
	}
	if image.Entry != (v2input.Entry{Contract: "WorkingImage", Ref: edge.Image}) {
		return v2input.ErrInvalid
	}
	supplied, err := ownExportCompositionDocument(ctx, image, image.Entry)
	if err != nil {
		return err
	}
	body := append([]byte(nil), raw...)
	original := append([]byte(nil), params...)
	fitBody := append([]byte(nil), fit.Body...)
	clone := func() ExportClosureNode {
		return ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fitBody...)}
	}
	origin, detectorParams, err := q.reader.ReadExportBodyFitDetectorParams(ctx, landmarkOrigin, parent, append([]byte(nil), body...), clone(), landmark, append([]byte(nil), original...), boxes)
	if err != nil {
		return err
	}
	fresh := func() (v2qualification.Expected, error) {
		var e v2qualification.Expected
		err := json.Unmarshal(q.expected, &e)
		return e, err
	}
	match := func() error {
		e, err := fresh()
		if err != nil {
			return err
		}
		return q.policy.MatchExportBodyFitDetectorProfile(ctx, origin, landmarkOrigin, parent, append([]byte(nil), body...), clone(), landmark, append([]byte(nil), original...), boxes, e)
	}
	e, err := fresh()
	if err != nil {
		return err
	}
	before, err := q.policy.ReadExportBodyFitDetectorQualification(ctx, q.reader.Objects, q.report, e)
	if err != nil {
		return err
	}
	if before.Report != q.report {
		return v2input.ErrInvalid
	}
	if err := match(); err != nil {
		return err
	}
	actual, err := q.images.ResolveExportNode(ctx, image.Entry)
	if err != nil {
		return err
	}
	imageDoc, err := ownExportCompositionDocument(ctx, actual, image.Entry)
	if err != nil {
		return err
	}
	if !bytes.Equal(imageDoc.Body, supplied.Body) {
		return v2input.ErrInvalid
	}
	if err := qualifyExportResolvedSourceWarps(ctx, actual, q.warps); err != nil {
		return err
	}
	node, err := q.documents.ResolveExportNode(ctx, boxes)
	if err != nil {
		return err
	}
	result, err := ownExportCompositionDocument(ctx, node, boxes)
	if err != nil {
		return err
	}
	if len(result.Body) > 2<<20 || origin.Output.Bytes != int64(len(result.Body)) {
		return v2input.ErrInvalid
	}
	bundle, err := canonical.Marshal(struct {
		Schema int                       `json:"schema_version"`
		Params exportCompositionDocument `json:"params"`
		Result exportCompositionDocument `json:"result"`
		Image  exportCompositionDocument `json:"image"`
	}{1, exportCompositionDocument{SHA: origin.Params.SHA256, Body: detectorParams}, result, imageDoc})
	if err != nil {
		return err
	}
	if _, err := q.native.ExportDocumentChildren(ctx, v2input.Entry{Contract: "FaceDetectorOrigin", Ref: v2input.Ref{ArtifactID: boxes.Ref.ArtifactID, SHA256: rawSHA(bundle)}}, bundle); err != nil {
		return err
	}
	final, finalParams, err := q.reader.ReadExportBodyFitDetectorParams(ctx, landmarkOrigin, parent, append([]byte(nil), body...), clone(), landmark, append([]byte(nil), original...), boxes)
	if err != nil {
		return err
	}
	if final != origin || !bytes.Equal(detectorParams, finalParams) {
		return v2input.ErrInvalid
	}
	for _, entry := range []v2input.Entry{image.Entry, boxes} {
		resolver := q.documents
		expected := result
		if entry == image.Entry {
			resolver = q.images
			expected = imageDoc
		}
		node, err := resolver.ResolveExportNode(ctx, entry)
		if err != nil {
			return err
		}
		doc, err := ownExportCompositionDocument(ctx, node, entry)
		if err != nil {
			return err
		}
		if !bytes.Equal(doc.Body, expected.Body) {
			return v2input.ErrInvalid
		}
		if entry == image.Entry {
			if err := qualifyExportResolvedSourceWarps(ctx, node, q.warps); err != nil {
				return err
			}
		}
	}
	e, err = fresh()
	if err != nil {
		return err
	}
	after, err := q.policy.ReadExportBodyFitDetectorQualification(ctx, q.reader.Objects, q.report, e)
	if err != nil {
		return err
	}
	if after != before {
		return v2input.ErrInvalid
	}
	if err := match(); err != nil {
		return err
	}
	return ctx.Err()
}

var _ ExportBodyFitFaceBoxesAuthority = (*QualifiedExportBodyFitFaceBoxes)(nil)
var _ ExportBodyFitDetectorPolicyReader = SQLExportDocumentCatalog{}
var _ ExportBodyFitDetectorPolicyReader = SQLExportResultInputCatalog{}
