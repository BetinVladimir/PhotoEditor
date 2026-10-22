package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RetouchPreviewPresentationLinkage verifies document/producer linkage only. Physical
// WorkingImage, pixels and PNG verification is required before signing URLs.
type RetouchPreviewPresentationLinkage struct {
	Document     PreviewPresentationDocument
	Presentation RetouchPreviewPresentation
	Evidence     VerifiedRetouchPreviewEvidence
	Sidecars     [7]PlanSource
}

func ReadRetouchPreviewPresentationLinkage(ctx context.Context, db previewOutputQueryer,
	store planObjectStore, bucket string, who auth.Identity, run uuid.UUID, params RetouchPreviewParams,
	adapterContext v2input.Ref, iccLockSHA, srgbSHA string) (RetouchPreviewPresentationLinkage, error) {
	empty := RetouchPreviewPresentationLinkage{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !shaPattern.MatchString(iccLockSHA) || !shaPattern.MatchString(srgbSHA) {
		return empty, ErrInvalid
	}
	doc, err := ReadRetouchPreviewPresentationDocument(ctx, db, store, bucket, who, run, params, adapterContext)
	if err != nil {
		return empty, err
	}
	body, digest, err := FreezeRetouchPreviewParams(params)
	if err != nil {
		return empty, err
	}
	load := func(ctx context.Context) ([7]PlanSource, error) {
		var out [7]PlanSource
		rows, err := db.Query(ctx, "SELECT role,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_retouch_preview_presentation_sidecars($1,$2,$3,$4,$5,$6,$7,$8,$9)",
			params.TenantID, run, who.UserID, who.SessionID, string(body), digest, adapterContext.ArtifactID, adapterContext.SHA256, string(doc.raw))
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
			if !validRetouchPreviewSidecar(index, s) {
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
	evidence, err := DecodeRetouchPreviewEvidence(sidecars[0].Ref, raw, params)
	if err != nil {
		return empty, err
	}
	verified := VerifiedRetouchPreviewEvidence{TaskID: doc.TaskID, ResultID: doc.ResultID, Evidence: evidence, Source: sidecars[0], raw: append([]byte(nil), raw...)}
	presentation, err := DecodeRetouchPreviewPresentation(doc.Source.Ref, doc.raw, verified, iccLockSHA, srgbSHA)
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
	latest, err := ReadRetouchPreviewPresentationDocument(ctx, db, store, bucket, who, run, params, adapterContext)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if current != sidecars || latest.Source != doc.Source || latest.TaskID != doc.TaskID || latest.ResultID != doc.ResultID || latest.Attempt != doc.Attempt || latest.Fence != doc.Fence {
		return empty, ErrConflict
	}
	return RetouchPreviewPresentationLinkage{Document: doc, Presentation: presentation, Evidence: verified, Sidecars: sidecars}, nil
}

// Role-specific byte ceilings bound JSON, full-resolution pixels, and display
// media independently; a declared content type cannot select another budget.
func validRetouchPreviewSidecar(index int, source PlanSource) bool {
	if index < 0 || index >= 7 || source.Bytes < 2 {
		return false
	}
	media, maximum := "application/json", int64(1048576)
	if index == 2 {
		media, maximum = "application/x-npy", 2400016384
	} else if index >= 3 {
		media, maximum = "image/png", 32833536
		if index <= 4 { maximum = 33882112 }
	}
	return source.MediaType == media && source.Bytes <= maximum
}
