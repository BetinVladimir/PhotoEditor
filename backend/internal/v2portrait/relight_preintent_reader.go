package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type SQLRelightPreintentReader struct {
	DB      DonorConfirmationTransactions
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}

type RelightPreintentDocuments struct {
	Origin                             RelightPreintentProducer
	SelectedBody, RootBody, ParamsBody []byte
	// ExternalVectors is a required unresolved dependency, not verified evidence.
	ExternalVectors *v2input.Ref
}

func (s SQLRelightPreintentReader) lookup(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, base v2input.SessionVersionBinding, selected v2input.Ref, kind string) (RelightPreintentProducer, error) {
	var zero RelightPreintentProducer
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	origin, err := DiscoverRelightPreintentProducer(ctx, tx, who, tenant, project, snapshot, base, selected, kind, s.Bucket)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return origin, nil
}

// Load reads original documents before immutable intent creation. It does not
// prove qualification or mask pixels. External vectors are explicitly unresolved
// until independently discovered/read/verified; callers must never omit them.
func (s SQLRelightPreintentReader) Load(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, params RelightParams, sourceRaw []byte, kind string) (RelightPreintentDocuments, error) {
	var zero RelightPreintentDocuments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return zero, v2input.ErrInvalid
	}
	raw, _, err := params.Compile()
	if err != nil {
		return zero, err
	}
	var owned RelightParams
	if json.Unmarshal(raw, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	params = owned
	if len(sourceRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	sourceRaw = append([]byte(nil), sourceRaw...)
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return zero, err
	}
	selected := params.Subject
	switch kind {
	case "depth":
		selected = params.Depth
	case "person", "manual_target":
	case "manual_guard":
		selected = params.Protected
	default:
		return zero, v2input.ErrInvalid
	}
	before, err := s.lookup(ctx, who, tenant, project, snapshot, params.Base, selected, kind)
	if err != nil {
		return zero, err
	}
	reader := SQLRelightManualReader{Objects: s.Objects}
	selectedBody, err := reader.document(ctx, before.Selected)
	if err != nil {
		return zero, err
	}
	rootBody := selectedBody
	if before.Root != before.Selected {
		rootBody, err = reader.document(ctx, before.Root)
		if err != nil {
			return zero, err
		}
	}
	originalBody, err := reader.document(ctx, before.Params)
	if err != nil {
		return zero, err
	}
	original := manualLocatorRef(before.Params)
	var external *v2input.Ref
	if kind == "depth" {
		depth, e := review.ParseRelativeDepth(selectedBody)
		if e != nil {
			return zero, e
		}
		normalized, e := canonical.Marshal(depth)
		if e != nil || !bytes.Equal(normalized, selectedBody) || depth.Canvas != source.Canvas || !bytes.Equal(depth.TransformChain, source.TransformChain) {
			return zero, v2input.ErrInvalid
		}
		if err = VerifyRelightDepthProducerParams(params, original, originalBody); err != nil {
			return zero, err
		}
	} else {
		mask, e := review.ParseMaskRef(selectedBody)
		if e != nil {
			return zero, e
		}
		normalized, e := canonical.Marshal(mask)
		if e != nil || !bytes.Equal(normalized, selectedBody) || mask.Canvas != source.Canvas || !bytes.Equal(mask.TransformChain, source.TransformChain) || mask.Protected != (kind == "manual_guard") {
			return zero, v2input.ErrInvalid
		}
		if kind == "person" {
			if mask.Selector != "person" {
				return zero, v2input.ErrInvalid
			}
			if err = VerifyRelightPersonProducerParams(params, original, originalBody); err != nil {
				return zero, err
			}
		} else {
			if mask.Selector != "manual" {
				return zero, v2input.ErrInvalid
			}
			rootRef := manualLocatorRef(before.Root)
			root, e := review.ParseManualRasterDocument(rootBody, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256})
			if e != nil {
				return zero, e
			}
			declared := root.Target
			if kind == "manual_guard" {
				declared = root.Protected
			}
			declaredBody, e := canonical.Marshal(declared)
			if e != nil || !bytes.Equal(declaredBody, selectedBody) {
				return zero, v2input.ErrInvalid
			}
			vector, e := review.VerifyManualMaskParams(originalBody, review.ArtifactRef{ID: original.ArtifactID, SHA256: original.SHA256}, rootBody, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256}, root.Target, source.Source(review.ArtifactRef{ID: params.Base.Source.ArtifactID, SHA256: params.Base.Source.SHA256}))
			if e != nil {
				return zero, e
			}
			if vector != nil {
				external = &v2input.Ref{ArtifactID: vector.ID, SHA256: vector.SHA256}
			}
		}
	}
	after, err := s.lookup(ctx, who, tenant, project, snapshot, params.Base, selected, kind)
	if err != nil {
		return zero, err
	}
	if before != after {
		return zero, v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return RelightPreintentDocuments{Origin: before, SelectedBody: append([]byte(nil), selectedBody...), RootBody: append([]byte(nil), rootBody...), ParamsBody: append([]byte(nil), originalBody...), ExternalVectors: external}, nil
}
