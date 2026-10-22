package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

// PreviewPresentationLinkage verifies document/producer linkage only. Physical
// WorkingImage, pixels and PNG verification is required before signing URLs.
type PreviewPresentationLinkage struct {
	Document     PreviewPresentationDocument
	Presentation ExposurePreviewPresentation
	Evidence     VerifiedPreviewEvidence
	Sidecars     [7]PlanSource
}

func ReadExposurePreviewPresentationLinkage(ctx context.Context, db previewOutputQueryer,
	store planObjectStore, bucket string, who auth.Identity, run uuid.UUID, params ExposurePreviewParams,
	iccLockSHA, srgbSHA string) (PreviewPresentationLinkage, error) {
	empty := PreviewPresentationLinkage{}
	doc, err := ReadExposurePreviewPresentationDocument(ctx, db, store, bucket, who, run, params)
	if err != nil {
		return empty, err
	}
	body, digest, err := FreezeExposurePreviewParams(params)
	if err != nil {
		return empty, err
	}
	load := func(ctx context.Context) ([7]PlanSource, error) {
		var out [7]PlanSource
		rows, err := db.Query(ctx, "SELECT role,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_preapply_preview_presentation_sidecars($1,$2,$3,$4,$5,$6,$7)",
			params.TenantID, run, who.UserID, who.SessionID, string(body), digest, string(doc.raw))
		if err != nil {
			return out, err
		}
		defer rows.Close()
		roles := map[string]int{"preview_evidence": 0, "working_image": 1, "pixels": 2, "before": 3, "after": 4, "affected": 5, "protected": 6}
		seen := [7]bool{}
		ids := map[uuid.UUID]bool{doc.Source.Ref.ArtifactID: true}
		for rows.Next() {
			var role string
			var s PlanSource
			if err := rows.Scan(&role, &s.Ref.ArtifactID, &s.Ref.SHA256, &s.Bucket, &s.Key, &s.Version, &s.Bytes, &s.MediaType); err != nil {
				return out, err
			}
			index, ok := roles[role]
			if !ok || seen[index] || ids[s.Ref.ArtifactID] || s.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(s.Ref.SHA256) || s.Bucket != bucket || s.Key == "" || s.Version == "" || s.Version == "null" || s.Bytes < 2 {
				return out, ErrInvalid
			}
			media := "application/json"
			maximum := int64(1048576)
			if index == 2 {
				media = "application/x-npy"
				maximum = 2400016384
			}
			if index >= 3 {
				media = "image/png"
				maximum = 32833536
				if index <= 4 {
					maximum = 33882112
				}
			}
			if s.MediaType != media || s.Bytes > maximum {
				return out, ErrInvalid
			}
			seen[index] = true
			ids[s.Ref.ArtifactID] = true
			out[index] = s
		}
		if err := rows.Err(); err != nil {
			return out, err
		}
		for _, present := range seen {
			if !present {
				return out, ErrUnavailable
			}
		}
		return out, nil
	}
	sidecars, err := load(ctx)
	if err != nil {
		return empty, err
	}
	read, err := NewPlanObjectReader(store, bucket)
	if err != nil {
		return empty, err
	}
	raw, err := read(ctx, sidecars[0])
	if err != nil {
		return empty, err
	}
	if int64(len(raw)) != sidecars[0].Bytes {
		return empty, ErrInvalid
	}
	evidence, err := DecodeExposurePreviewEvidence(sidecars[0].Ref, raw, params)
	if err != nil {
		return empty, err
	}
	verified := VerifiedPreviewEvidence{TaskID: doc.TaskID, ResultID: doc.ResultID, Evidence: evidence, Source: sidecars[0], raw: append([]byte(nil), raw...)}
	presentation, err := DecodeExposurePreviewPresentation(doc.Source.Ref, doc.raw, verified, iccLockSHA, srgbSHA)
	if err != nil {
		return empty, err
	}
	expected := []PlanSource{{Ref: presentation.NumericalEvidence}, {Ref: presentation.WorkingImage}, {Ref: presentation.Pixels}, {Ref: presentation.Before}, {Ref: presentation.After}, {Ref: presentation.Affected}, {Ref: presentation.Protected}}
	for i := range sidecars {
		if sidecars[i].Ref != expected[i].Ref {
			return empty, ErrInvalid
		}
	}
	current, err := load(ctx)
	if err != nil {
		return empty, err
	}
	// Re-read canonical authority: a new result with identical sidecar rows must
	// still invalidate the original attempt and its presentation document.
	latest, err := ReadExposurePreviewPresentationDocument(ctx, db, store, bucket, who, run, params)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if current != sidecars || latest.Source != doc.Source || latest.TaskID != doc.TaskID || latest.ResultID != doc.ResultID || latest.Attempt != doc.Attempt || latest.Fence != doc.Fence {
		return empty, ErrConflict
	}
	return PreviewPresentationLinkage{Document: doc, Presentation: presentation, Evidence: verified, Sidecars: sidecars}, nil
}
