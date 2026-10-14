package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) defectLandmarkObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, ref v2input.Ref) (objectstore.Object, error) {
	var object objectstore.Object
	if ref.ArtifactID == uuid.Nil {
		return object, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(ref.SHA256); err != nil {
		return object, v2input.ErrInvalid
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return object, err
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, "SELECT bucket,object_key,object_version,bytes FROM photo.semantic_proposal_defect_landmark_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, ref.ArtifactID, ref.SHA256).Scan(&bucket, &object.Key, &object.VersionID, &object.Bytes)
		if err == pgx.ErrNoRows {
			return v2input.ErrInvalid
		}
		if err != nil {
			return err
		}
		if bucket != s.Bucket || object.Bytes < 2 || object.Bytes > 1048576 {
			return v2input.ErrInvalid
		}
		object.SHA256 = ref.SHA256
		object.ContentType = "application/json"
		return nil
	})
	if err != nil {
		return objectstore.Object{}, err
	}
	return object, nil
}

// ReadDefectFaces resolves the full face set from canonical declared landmark
// producer outputs and rechecks exact catalog pins after IO. Qualification and
// face mask/protection completeness are separate; no activation is granted.
// Legacy standalone Face68 currently has no supported canonical output role and
// fails closed through the catalog gate instead of accepting arbitrary JSON.
func (s SQLTransferSourceReader) ReadDefectFaces(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins) ([]review.Face68, error) {
	return s.readDefectFaces(ctx, who, tenantID, binding, pins, nil)
}

type defectFaceVerifier func(context.Context, v2input.Ref, v2input.Ref, json.RawMessage, json.RawMessage) error

func (s SQLTransferSourceReader) readDefectFaces(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, verify defectFaceVerifier) ([]review.Face68, error) {
	evidence, err := s.ReadDefectEvidence(ctx, who, tenantID, binding, pins)
	if err != nil {
		return nil, err
	}
	raw, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return nil, err
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		return nil, err
	}
	declaredLandmarks, err := review.ProposalLandmarkRefs(evidence.Report, review.ArtifactRef{ID: pins.Proposal.ArtifactID, SHA256: pins.Proposal.SHA256}, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil {
		return nil, err
	}
	used := map[v2input.Ref]objectstore.Object{}
	type detectionLink struct{ Landmark, Detection v2input.Ref }
	detections := map[detectionLink]objectstore.Object{}
	faceParams := map[detectionLink][2]defectEvidenceObject{}
	faces, err := review.ResolveProposalFaces(ctx, evidence.Report, review.ArtifactRef{ID: pins.Proposal.ArtifactID, SHA256: pins.Proposal.SHA256}, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}), func(ctx context.Context, ref review.ArtifactRef) ([]byte, error) {
		pin := v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
		object, err := s.defectLandmarkObject(ctx, who, tenantID, binding, pins, pin)
		if err != nil {
			return nil, err
		}
		if old, exists := used[pin]; exists && old != object {
			return nil, v2input.ErrConflict
		}
		body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, pin)
		if err != nil {
			return nil, err
		}
		var projection struct {
			Faces v2input.Ref `json:"faces"`
		}
		if json.Unmarshal(body, &projection) != nil {
			return nil, v2input.ErrInvalid
		}
		detectionObject, err := s.defectDetectionObject(ctx, who, tenantID, binding, pins, pin, projection.Faces)
		if err != nil {
			return nil, err
		}
		link := detectionLink{Landmark: pin, Detection: projection.Faces}
		if old, exists := detections[link]; exists && old != detectionObject {
			return nil, v2input.ErrConflict
		}
		detectionBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, detectionObject, projection.Faces)
		if err != nil {
			return nil, err
		}
		if err := review.VerifyLandmarkDetection(body, review.ArtifactRef{ID: projection.Faces.ArtifactID, SHA256: projection.Faces.SHA256}, detectionBody, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})); err != nil {
			return nil, err
		}
		params, err := s.defectFaceParamsObjects(ctx, who, tenantID, binding, pins, pin, projection.Faces)
		if err != nil {
			return nil, err
		}
		if old, exists := faceParams[link]; exists && old != params {
			return nil, v2input.ErrConflict
		}
		var paramsBodies [2][]byte
		for i, entry := range params {
			paramsBodies[i], err = v2input.ReadPinnedSemanticDocument(ctx, s.Objects, entry.object, entry.ref)
			if err != nil {
				return nil, err
			}
		}
		source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
		if err := review.VerifyLandmarkParams(review.ArtifactRef{ID: params[0].ref.ArtifactID, SHA256: params[0].ref.SHA256}, paramsBodies[0], body, source); err != nil {
			return nil, err
		}
		if err := review.VerifyFaceDetectParams(review.ArtifactRef{ID: params[1].ref.ArtifactID, SHA256: params[1].ref.SHA256}, paramsBodies[1], detectionBody, source); err != nil {
			return nil, err
		}
		if verify != nil {
			if err := verify(ctx, pin, projection.Faces, body, detectionBody); err != nil {
				return nil, err
			}
		}
		faceParams[link] = params
		detections[link] = detectionObject
		used[pin] = object
		return body, nil
	})
	if err != nil {
		return nil, err
	}
	if len(used) != len(declaredLandmarks) {
		return nil, v2input.ErrInvalid
	}
	for _, declared := range declaredLandmarks {
		ref := v2input.Ref{ArtifactID: declared.ID, SHA256: declared.SHA256}
		object, exists := used[ref]
		if !exists {
			return nil, v2input.ErrInvalid
		}

		current, err := s.defectLandmarkObject(ctx, who, tenantID, binding, pins, ref)
		if err != nil {
			return nil, err
		}
		if current != object {
			return nil, v2input.ErrConflict
		}
	}
	for link, objects := range faceParams {
		current, err := s.defectFaceParamsObjects(ctx, who, tenantID, binding, pins, link.Landmark, link.Detection)
		if err != nil {
			return nil, err
		}
		if current != objects {
			return nil, v2input.ErrConflict
		}
	}
	for link, object := range detections {
		current, err := s.defectDetectionObject(ctx, who, tenantID, binding, pins, link.Landmark, link.Detection)
		if err != nil {
			return nil, err
		}
		if current != object {
			return nil, v2input.ErrConflict
		}
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return faces, nil
}
func (s SQLTransferSourceReader) defectDetectionObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, ref, detection v2input.Ref) (objectstore.Object, error) {
	var object objectstore.Object
	if ref.ArtifactID == uuid.Nil || detection.ArtifactID == uuid.Nil {
		return object, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(ref.SHA256); err != nil {
		return object, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(detection.SHA256); err != nil {
		return object, v2input.ErrInvalid
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return object, err
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, "SELECT bucket,object_key,object_version,bytes FROM photo.semantic_proposal_defect_detection_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, ref.ArtifactID, ref.SHA256, detection.ArtifactID, detection.SHA256).Scan(&bucket, &object.Key, &object.VersionID, &object.Bytes)
		if err == pgx.ErrNoRows {
			return v2input.ErrInvalid
		}
		if err != nil {
			return err
		}
		if bucket != s.Bucket || object.Bytes < 2 || object.Bytes > 1048576 {
			return v2input.ErrInvalid
		}
		object.SHA256 = detection.SHA256
		object.ContentType = "application/json"
		return nil
	})
	if err != nil {
		return objectstore.Object{}, err
	}
	return object, nil
}

func (s SQLTransferSourceReader) defectFaceParamsObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, landmark, detection v2input.Ref) ([2]defectEvidenceObject, error) {
	var result [2]defectEvidenceObject
	if _, err := s.defectDetectionObject(ctx, who, tenantID, binding, pins, landmark, detection); err != nil {
		return result, err
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT contract,artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_defect_face_params_objects($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, landmark.ArtifactID, landmark.SHA256, detection.ArtifactID, detection.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		contracts := [2]string{"LandmarkParams", "FaceDetectParams"}
		for rows.Next() {
			if count >= 2 {
				return v2input.ErrInvalid
			}
			var contract, bucket string
			entry := &result[count]
			if err := rows.Scan(&contract, &entry.ref.ArtifactID, &entry.ref.SHA256, &bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes); err != nil {
				return err
			}
			if contract != contracts[count] || bucket != s.Bucket {
				return v2input.ErrInvalid
			}
			entry.object.SHA256 = entry.ref.SHA256
			entry.object.ContentType = "application/json"
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 2 {
			return v2input.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return [2]defectEvidenceObject{}, err
	}
	return result, nil
}
