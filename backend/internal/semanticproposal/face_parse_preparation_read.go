package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ReadFaceParsePreparation verifies selected original parser/detector documents,
// Params and the complete uint16 labels stream, without a defect producer.
// This is preflight only; qualification, RGB/ICC closure and transaction locks
// remain mandatory before admission. Failure returns no partial metadata/counts.
func (s SQLTransferSourceReader) ReadFaceParsePreparation(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, parse v2input.Ref) (review.FaceLabels19, [19]int64, error) {
	labels, counts, _, err := s.readFaceParsePreparationPinned(ctx, who, tenantID, binding, parse)
	return labels, counts, err
}

func (s SQLTransferSourceReader) readFaceParsePreparationPinned(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, parse v2input.Ref) (review.FaceLabels19, [19]int64, defectEvidenceObject, error) {
	fail := func(err error) (review.FaceLabels19, [19]int64, defectEvidenceObject, error) {
		return review.FaceLabels19{}, [19]int64{}, defectEvidenceObject{}, err
	}
	parser, err := s.facePreparationObjects(ctx, who, tenantID, binding, parse, "face.parse")
	if err != nil {
		return fail(err)
	}
	sourceRaw, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return fail(err)
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return fail(err)
	}
	asReview := func(r v2input.Ref) review.ArtifactRef { return review.ArtifactRef{ID: r.ArtifactID, SHA256: r.SHA256} }
	source := image.Source(asReview(binding.Source))
	read := func(p defectEvidenceObject) ([]byte, error) {
		return v2input.ReadPinnedSemanticDocument(ctx, s.Objects, p.object, p.ref)
	}
	manifest, err := read(parser[0])
	if err != nil {
		return fail(err)
	}
	var projection struct {
		Faces  v2input.Ref `json:"faces"`
		Labels v2input.Ref `json:"labels"`
	}
	if json.Unmarshal(manifest, &projection) != nil {
		return fail(v2input.ErrInvalid)
	}
	detector, err := s.facePreparationObjects(ctx, who, tenantID, binding, projection.Faces, "face.detect")
	if err != nil {
		return fail(err)
	}
	pixels, err := s.faceParsePreparationPixels(ctx, who, tenantID, binding, parse, projection.Faces, projection.Labels)
	if err != nil {
		return fail(err)
	}
	parseParams, err := read(parser[1])
	if err != nil {
		return fail(err)
	}
	detectionRaw, err := read(detector[0])
	if err != nil {
		return fail(err)
	}
	detectionParams, err := read(detector[1])
	if err != nil {
		return fail(err)
	}
	labels, err := review.VerifyFaceParseParams(parseParams, asReview(parser[1].ref), manifest, asReview(parse), detectionRaw, asReview(projection.Faces), source)
	if err != nil {
		return fail(err)
	}
	if labels.Labels != asReview(pixels.ref) {
		return fail(v2input.ErrInvalid)
	}
	if err := review.VerifyFaceDetectParams(asReview(detector[1].ref), detectionParams, detectionRaw, source); err != nil {
		return fail(err)
	}
	stream, err := s.Objects.Open(ctx, pixels.object)
	if err != nil {
		return fail(err)
	}
	counts, verifyErr := masknpy.VerifyFaceLabelsNPY(ctx, masknpy.PinnedMask{Reader: stream, Bytes: pixels.object.Bytes, SHA256: pixels.ref.SHA256}, int64(source.Canvas.Width), int64(source.Canvas.Height))
	closeErr := stream.Close()
	if verifyErr != nil {
		return fail(verifyErr)
	}
	if closeErr != nil {
		return fail(closeErr)
	}
	if labels.ParsedFaces == 0 && counts[0] != int64(source.Canvas.Width)*int64(source.Canvas.Height) {
		return fail(v2input.ErrInvalid)
	}
	currentParser, err := s.facePreparationObjects(ctx, who, tenantID, binding, parse, "face.parse")
	if err != nil {
		return fail(err)
	}
	currentDetector, err := s.facePreparationObjects(ctx, who, tenantID, binding, projection.Faces, "face.detect")
	if err != nil {
		return fail(err)
	}
	currentPixels, err := s.faceParsePreparationPixels(ctx, who, tenantID, binding, parse, projection.Faces, projection.Labels)
	if err != nil {
		return fail(err)
	}
	if currentParser != parser || currentDetector != detector || currentPixels != pixels {
		return fail(v2input.ErrConflict)
	}
	currentSource, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return fail(err)
	}
	if !bytes.Equal(currentSource, sourceRaw) {
		return fail(v2input.ErrConflict)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return labels, counts, pixels, nil
}
