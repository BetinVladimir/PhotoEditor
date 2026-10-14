package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type defectSkinReadProof struct {
	Pixel, Parse, Detection                                            v2input.Ref
	ParseModelLock, ParseBackend, DetectionModelLock, DetectionBackend string
	PNG                                                                defectEvidenceObject
	ParseObjects                                                       defectSkinParseObjects
	DetectionObjects                                                   defectSkinDetectionObjects
}

// verifyDefectSkinPixels verifies original producer documents and exact physical
// derivation. It does not certify model inference or qualification/admission.
func (s SQLTransferSourceReader) verifyDefectSkinPixels(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, objects defectMaskObjects, paramsBody []byte, mask review.ProtectionMask, source review.DefectSource) (defectSkinReadProof, error) {
	empty := defectSkinReadProof{}
	var projection struct {
		Labels v2input.Ref `json:"labels"`
	}
	if json.Unmarshal(paramsBody, &projection) != nil {
		return empty, v2input.ErrInvalid
	}
	proof := defectSkinReadProof{Pixel: objects.Pixel.ref, Parse: projection.Labels}
	parseObjects, err := s.defectSkinObjects(ctx, who, tenantID, binding, pins, proof.Pixel, proof.Parse)
	if err != nil {
		return empty, err
	}
	proof.ParseObjects = parseObjects
	read := func(entry defectEvidenceObject) ([]byte, error) {
		return v2input.ReadPinnedSemanticDocument(ctx, s.Objects, entry.object, entry.ref)
	}
	manifest, err := read(parseObjects.Manifest)
	if err != nil {
		return empty, err
	}
	ref := func(r v2input.Ref) review.ArtifactRef { return review.ArtifactRef{ID: r.ArtifactID, SHA256: r.SHA256} }
	labels, err := review.VerifyFaceSkinParams(paramsBody, ref(objects.Params.ref), manifest, ref(proof.Parse), mask, source)
	if err != nil {
		return empty, err
	}
	if ref(parseObjects.Pixels.ref) != labels.Labels {
		return empty, v2input.ErrInvalid
	}
	proof.ParseModelLock = labels.ModelLockID
	proof.ParseBackend = labels.Backend
	proof.Detection = v2input.Ref{ArtifactID: labels.Faces.ID, SHA256: labels.Faces.SHA256}
	detectionObjects, err := s.defectSkinDetectionObjects(ctx, who, tenantID, binding, pins, proof.Pixel, proof.Parse, proof.Detection)
	if err != nil {
		return empty, err
	}
	proof.DetectionObjects = detectionObjects
	parseParams, err := read(parseObjects.Params)
	if err != nil {
		return empty, err
	}
	detection, err := read(detectionObjects.Detection)
	if err != nil {
		return empty, err
	}
	detectionParams, err := read(detectionObjects.Params)
	if err != nil {
		return empty, err
	}
	if _, err := review.VerifyFaceParseParams(parseParams, ref(parseObjects.Params.ref), manifest, ref(proof.Parse), detection, ref(proof.Detection), source); err != nil {
		return empty, err
	}
	boxes, err := review.ParseFaceBoxes(detection, source)
	if err != nil {
		return empty, err
	}
	proof.DetectionModelLock = boxes.ModelLockID
	proof.DetectionBackend = boxes.Backend
	if err := review.VerifyFaceDetectParams(ref(detectionObjects.Params.ref), detectionParams, detection, source); err != nil {
		return empty, err
	}
	left, err := s.Objects.Open(ctx, parseObjects.Pixels.object)
	if err != nil {
		return empty, err
	}
	right, err := s.Objects.Open(ctx, objects.Pixel.object)
	if err != nil {
		left.Close()
		return empty, err
	}
	count, verifyErr := masknpy.VerifyFaceSkinNPY(ctx, masknpy.PinnedMask{Reader: left, Bytes: parseObjects.Pixels.object.Bytes, SHA256: parseObjects.Pixels.ref.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: objects.Pixel.object.Bytes, SHA256: objects.Pixel.ref.SHA256}, source.Canvas.Width, source.Canvas.Height)
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return empty, verifyErr
	}
	if leftErr != nil {
		return empty, leftErr
	}
	if rightErr != nil {
		return empty, rightErr
	}
	if err := review.VerifyFaceSkinConfidence(mask, labels, count); err != nil {
		return empty, err
	}
	png, err := s.defectSkinPNGObject(ctx, who, tenantID, binding, pins, proof.Pixel)
	if err != nil {
		return empty, err
	}
	proof.PNG = png
	if err := s.verifyProbabilityPNG(ctx, objects.Pixel, png, source.Canvas.Width, source.Canvas.Height); err != nil {
		return empty, err
	}
	if err := s.recheckDefectSkin(ctx, who, tenantID, binding, pins, proof); err != nil {
		return empty, err
	}
	return proof, nil
}

func (s SQLTransferSourceReader) recheckDefectSkin(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, proof defectSkinReadProof) error {
	parse, err := s.defectSkinObjects(ctx, who, tenantID, binding, pins, proof.Pixel, proof.Parse)
	if err != nil {
		return err
	}
	if parse != proof.ParseObjects {
		return v2input.ErrConflict
	}
	detection, err := s.defectSkinDetectionObjects(ctx, who, tenantID, binding, pins, proof.Pixel, proof.Parse, proof.Detection)
	if err != nil {
		return err
	}
	if detection != proof.DetectionObjects {
		return v2input.ErrConflict
	}
	png, err := s.defectSkinPNGObject(ctx, who, tenantID, binding, pins, proof.Pixel)
	if err != nil {
		return err
	}
	if png != proof.PNG {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
