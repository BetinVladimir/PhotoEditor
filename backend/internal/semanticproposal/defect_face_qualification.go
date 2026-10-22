package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/facequalification"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func (s SQLTransferSourceReader) faceQualificationObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected, report v2input.Ref) (personEvidenceObject, error) {
	pin, err := (facequalification.Reader{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket, ProjectID: s.ProjectID}).Object(ctx, who, tenantID, expected, report)
	if err != nil {
		return personEvidenceObject{}, err
	}
	return personEvidenceObject{contract: pin.Contract, ref: pin.Ref, object: pin.Object, backend: pin.Backend}, nil
}

// ReadQualifiedDefectFaces verifies separately selected trusted reports for both
// face operations for one declared landmark collection, and checks stored
// profiles before/after IO. Other collections fail closed pending orchestration.
// Actual model measurements, full mask protection and admission locks remain
// independent obligations. A failure returns no partial face set.
func (s SQLTransferSourceReader) ReadQualifiedDefectFaces(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, landmark, detection v2input.Ref, reports [2]v2input.Ref, expected [2]v2qualification.Expected) ([]review.Face68, error) {
	for i := range expected {
		expected[i] = ownQualificationExpected(expected[i])
	}
	evidence, err := s.ReadDefectEvidence(ctx, who, tenantID, binding, pins)
	if err != nil {
		return nil, err
	}
	var declaration struct {
		Faces []struct {
			Landmarks v2input.Ref `json:"landmarks"`
		} `json:"faces"`
	}
	if json.Unmarshal(evidence.Params, &declaration) != nil || len(declaration.Faces) == 0 {
		return nil, v2qualification.ErrUnqualified
	}
	for _, face := range declaration.Faces {
		if face.Landmarks != landmark {
			return nil, v2qualification.ErrUnqualified
		}
	}
	var qualificationPins [2]personEvidenceObject
	if expected[0].Operation != "face.landmarks" || expected[1].Operation != "face.detect" {
		return nil, v2qualification.ErrUnqualified
	}
	for i := range expected {
		pin, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return nil, err
		}
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return nil, err
		}
		if _, err := v2qualification.VerifyTyped(raw, pin.ref.SHA256, expected[i]); err != nil {
			return nil, err
		}
		qualificationPins[i] = pin
	}
	if err := s.VerifyDefectFaceProducerProfiles(ctx, who, tenantID, binding, pins, landmark, detection, expected); err != nil {
		return nil, err
	}
	faces, err := s.ReadDefectFaces(ctx, who, tenantID, binding, pins)
	if err != nil {
		return nil, err
	}
	landmarkObject, err := s.defectLandmarkObject(ctx, who, tenantID, binding, pins, landmark)
	if err != nil {
		return nil, err
	}
	detectionObject, err := s.defectDetectionObject(ctx, who, tenantID, binding, pins, landmark, detection)
	if err != nil {
		return nil, err
	}
	for i, object := range [2]defectEvidenceObject{{ref: landmark, object: landmarkObject}, {ref: detection, object: detectionObject}} {
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object.object, object.ref)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			var bindingClaim struct {
				Faces v2input.Ref `json:"faces"`
			}
			if json.Unmarshal(raw, &bindingClaim) != nil || bindingClaim.Faces != detection {
				return nil, v2qualification.ErrUnqualified
			}
		}
		var claims struct {
			ModelLock string `json:"model_lock_id"`
			Backend   string `json:"backend"`
		}
		if json.Unmarshal(raw, &claims) != nil || claims.ModelLock != expected[i].ModelLockSHA || claims.Backend != expected[i].Backend {
			return nil, v2qualification.ErrUnqualified
		}
	}
	currentLandmark, err := s.defectLandmarkObject(ctx, who, tenantID, binding, pins, landmark)
	if err != nil {
		return nil, err
	}
	if currentLandmark != landmarkObject {
		return nil, v2input.ErrConflict
	}
	currentDetection, err := s.defectDetectionObject(ctx, who, tenantID, binding, pins, landmark, detection)
	if err != nil {
		return nil, err
	}
	if currentDetection != detectionObject {
		return nil, v2input.ErrConflict
	}
	for i := range expected {
		current, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return nil, err
		}
		if current != qualificationPins[i] {
			return nil, v2input.ErrConflict
		}
	}
	if err := s.VerifyDefectFaceProducerProfiles(ctx, who, tenantID, binding, pins, landmark, detection, expected); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return faces, nil
}

// ReadQualifiedDefectFaceSet derives every collection from original Params and
// applies the independently selected operation policies to every producer. It
// returns no partial set. Qualification is preflight; final admission must lock
// the retained catalog/policy evidence. An empty set proves no declared faces,
// not absence of people in the source image.
func (s SQLTransferSourceReader) ReadQualifiedDefectFaceSet(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, reports [2]v2input.Ref, expected [2]v2qualification.Expected) ([]review.Face68, error) {
	faces, _, err := s.readQualifiedDefectFaceEvidence(ctx, who, tenantID, binding, pins, reports, expected)
	return faces, err
}

func (s SQLTransferSourceReader) readQualifiedDefectFaceEvidence(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, reports [2]v2input.Ref, expected [2]v2qualification.Expected) ([]review.Face68, []facePreparationProof, error) {
	for i := range expected {
		expected[i] = ownQualificationExpected(expected[i])
	}
	if expected[0].Operation != "face.landmarks" || expected[1].Operation != "face.detect" {
		return nil, nil, v2qualification.ErrUnqualified
	}
	var qualificationPins [2]personEvidenceObject
	for i := range expected {
		pin, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return nil, nil, err
		}
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return nil, nil, err
		}
		if _, err := v2qualification.VerifyTyped(raw, pin.ref.SHA256, expected[i]); err != nil {
			return nil, nil, err
		}
		qualificationPins[i] = pin
	}
	type link struct{ Landmark, Detection v2input.Ref }
	links := make([]link, 0)
	proofs := make([]facePreparationProof, 0)
	expectedBody, err := canonical.Marshal(expected)
	if err != nil {
		return nil, nil, err
	}
	seen := map[link]bool{}
	faces, err := s.readDefectFaces(ctx, who, tenantID, binding, pins, func(ctx context.Context, landmark, detection v2input.Ref, landmarkBody, detectionBody json.RawMessage) error {
		pair := link{Landmark: landmark, Detection: detection}
		if err := s.VerifyDefectFaceProducerProfiles(ctx, who, tenantID, binding, pins, landmark, detection, expected); err != nil {
			return err
		}
		for i, raw := range [2]json.RawMessage{landmarkBody, detectionBody} {
			var claims struct {
				ModelLock string `json:"model_lock_id"`
				Backend   string `json:"backend"`
			}
			if json.Unmarshal(raw, &claims) != nil || claims.ModelLock != expected[i].ModelLockSHA || claims.Backend != expected[i].Backend {
				return v2qualification.ErrUnqualified
			}
		}

		if !seen[pair] {
			proof := facePreparationProof{tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: binding, qualification: qualificationPins, expected: string(expectedBody)}
			for i, ref := range [2]v2input.Ref{landmark, detection} {
				objects, err := s.facePreparationObjects(ctx, who, tenantID, binding, ref, expected[i].Operation)
				if err != nil {
					return err
				}
				proof.objects[i] = objects
			}
			if !validFacePreparationProofScope(s, who, tenantID, binding, proof) {
				return v2input.ErrInvalid
			}
			proofs = append(proofs, proof)
			seen[pair] = true
			links = append(links, pair)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for _, pair := range links {
		if err := s.VerifyDefectFaceProducerProfiles(ctx, who, tenantID, binding, pins, pair.Landmark, pair.Detection, expected); err != nil {
			return nil, nil, err
		}
	}
	for _, proof := range proofs {
		if err := s.recheckFacePreparationProof(ctx, who, tenantID, binding, proof); err != nil {
			return nil, nil, err
		}
	}
	for i := range expected {
		current, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return nil, nil, err
		}
		if current != qualificationPins[i] {
			return nil, nil, v2input.ErrConflict
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return faces, proofs, nil
}
