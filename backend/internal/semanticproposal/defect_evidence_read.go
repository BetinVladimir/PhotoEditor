package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type defectEvidenceObject struct {
	ref    v2input.Ref
	object objectstore.Object
}

// VerifiedDefectEvidence proves original document consistency and live catalog
// origin, physical NPY integrity and exact PNG label equivalence.
// Qualification and admission require separate gates.
type VerifiedDefectEvidence struct {
	Params, Proposal, Report     json.RawMessage
	Labels, Protection, LabelPNG v2input.Ref
	FaceMasks                    []review.ProposalFaceMask
}

func (s SQLTransferSourceReader) defectEvidenceObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins) ([]defectEvidenceObject, error) {
	if s.Objects == nil || s.Bucket == "" {
		return nil, v2input.ErrInvalid
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return nil, err
	}
	result := make([]defectEvidenceObject, 0, 5)
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		for i, ref := range []v2input.Ref{pins.Params, pins.Proposal, pins.Report, pins.Labels, pins.Protection} {
			entry := defectEvidenceObject{ref: ref}
			var bucket string
			err := tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND sha256=$3 AND state='ready' AND verified_at IS NOT NULL AND (expires_at IS NULL OR expires_at>clock_timestamp())`, tenantID, ref.ArtifactID, ref.SHA256).Scan(&bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes, &entry.object.ContentType)
			if err != nil {
				return err
			}
			media, minimum, maximum := "application/json", int64(2), int64(1048576)
			if i >= 3 {
				media, minimum, maximum = "application/octet-stream", 32, 800065548
			}
			if bucket != s.Bucket || entry.object.ContentType != media || entry.object.Bytes < minimum || entry.object.Bytes > maximum || entry.object.VersionID == "" || entry.object.VersionID == "null" {
				return v2input.ErrInvalid
			}
			entry.object.SHA256 = ref.SHA256
			result = append(result, entry)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReadDefectEvidence checks exact-source canonical producer before IO and repeats
// both authority and exact object pins afterwards. It never returns partial evidence.
func (s SQLTransferSourceReader) ReadDefectEvidence(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins) (VerifiedDefectEvidence, error) {
	empty := VerifiedDefectEvidence{}
	objects, err := s.defectEvidenceObjects(ctx, who, tenantID, binding, pins)
	if err != nil {
		return empty, err
	}
	image, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, err
	}
	bodies := make([]json.RawMessage, 3)
	for i, entry := range objects[:3] {
		bodies[i], err = v2input.ReadPinnedSemanticDocument(ctx, s.Objects, entry.object, entry.ref)
		if err != nil {
			return empty, err
		}
	}
	protection, err := v2input.VerifySemanticDefectEvidence(ctx, pins.Params, bodies[0], pins.Report, bodies[2], pins.Proposal, bodies[1], binding, image)
	if err != nil {
		return empty, err
	}
	labels, err := v2input.ResolveSemanticDefectLabels(ctx, pins.Proposal, bodies[1], binding, image)
	if err != nil {
		return empty, err
	}
	if protection != pins.Protection || labels != pins.Labels {
		return empty, v2input.ErrInvalid
	}
	parsedImage, err := review.ParseWorkingImage(image)
	if err != nil {
		return empty, err
	}
	faceMasks, err := review.ProposalFaceMasks(bodies[2], review.ArtifactRef{ID: pins.Proposal.ArtifactID, SHA256: pins.Proposal.SHA256}, parsedImage.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil {
		return empty, err
	}
	pngRef, err := review.ProposalLabelPNG(bodies[2], review.ArtifactRef{ID: pins.Proposal.ArtifactID, SHA256: pins.Proposal.SHA256}, parsedImage.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil {
		return empty, err
	}
	png, err := s.defectLabelPNGObject(ctx, who, tenantID, binding, pins, v2input.Ref{ArtifactID: pngRef.ID, SHA256: pngRef.SHA256})
	if err != nil {
		return empty, err
	}
	if err := s.verifyDefectLabelPNG(ctx, objects[3], png, parsedImage.Canvas.Width, parsedImage.Canvas.Height); err != nil {
		return empty, err
	}
	if err := s.verifyDefectPixels(ctx, binding, image, pins, bodies[1], objects[3], objects[4]); err != nil {
		return empty, err
	}
	declared, err := review.ProposalDeclaredProtections(bodies[2], review.ArtifactRef{ID: pins.Proposal.ArtifactID, SHA256: pins.Proposal.SHA256}, parsedImage.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil {
		return empty, err
	}
	requiredPins := make([]defectEvidenceObject, 0, len(declared))
	for _, mask := range declared {
		entry, err := s.defectDeclaredProtectionObject(ctx, who, tenantID, binding, pins, v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256})
		if err != nil {
			return empty, err
		}
		if err := s.verifyManualProtectionCoverage(ctx, entry, objects[4], parsedImage.Canvas.Width, parsedImage.Canvas.Height); err != nil {
			return empty, err
		}
		requiredPins = append(requiredPins, entry)
	}
	current, err := s.defectEvidenceObjects(ctx, who, tenantID, binding, pins)
	if err != nil {
		return empty, err
	}
	for i, entry := range objects {
		if entry != current[i] {
			return empty, v2input.ErrConflict
		}
	}
	currentPNG, err := s.defectLabelPNGObject(ctx, who, tenantID, binding, pins, png.ref)
	if err != nil {
		return empty, err
	}
	if currentPNG != png {
		return empty, v2input.ErrConflict
	}
	for _, entry := range requiredPins {
		current, err := s.defectDeclaredProtectionObject(ctx, who, tenantID, binding, pins, entry.ref)
		if err != nil {
			return empty, err
		}
		if current != entry {
			return empty, v2input.ErrConflict
		}
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return VerifiedDefectEvidence{Params: bodies[0], Proposal: bodies[1], Report: bodies[2], Labels: labels, Protection: protection, FaceMasks: faceMasks, LabelPNG: png.ref}, nil
}

func (s SQLTransferSourceReader) verifyDefectPixels(ctx context.Context, binding v2input.SessionVersionBinding, imageBody json.RawMessage, pins DefectOriginPins, proposalBody json.RawMessage, labels, protection defectEvidenceObject) error {
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return v2input.ErrInvalid
	}
	proposal, err := review.ReadDefectProposal(ctx, bytes.NewReader(proposalBody), review.ArtifactRef{ID: pins.Proposal.ArtifactID, SHA256: pins.Proposal.SHA256}, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil {
		return err
	}
	stream, err := s.Objects.Open(ctx, labels.object)
	if err != nil {
		return err
	}
	protectedStream, err := s.Objects.Open(ctx, protection.object)
	if err != nil {
		_ = stream.Close()
		return err
	}
	_, verifyErr := proposal.VerifyProtectedLabelMap(ctx,
		masknpy.PinnedMask{Reader: stream, Bytes: labels.object.Bytes, SHA256: pins.Labels.SHA256},
		masknpy.PinnedMask{Reader: protectedStream, Bytes: protection.object.Bytes, SHA256: pins.Protection.SHA256})
	closeLabelsErr := stream.Close()
	closeProtectionErr := protectedStream.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if closeLabelsErr != nil {
		return closeLabelsErr
	}
	return closeProtectionErr
}
