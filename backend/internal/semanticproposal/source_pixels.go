package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// VerifySourcePixels validates source RGB and optional alpha bytes without
// loading whole arrays. ICC qualification requires VerifyQualifiedSourcePixels.
func (s SQLTransferSourceReader) VerifySourcePixels(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding) (v2input.SemanticSourceMetadata, error) {
	return s.verifySourcePixels(ctx, who, tenantID, binding, false)
}

// VerifyFaceSourcePixels enforces the current face worker float32 unit-interval
// source input contract, preserving HDR support for other operation readers.
// It validates RGB, alpha and geometry pins; ICC qualification is separate.
func (s SQLTransferSourceReader) VerifyFaceSourcePixels(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding) (v2input.SemanticSourceMetadata, error) {
	return s.verifySourcePixels(ctx, who, tenantID, binding, true)
}

func (s SQLTransferSourceReader) verifySourcePixels(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, unitRGB bool) (v2input.SemanticSourceMetadata, error) {
	empty := v2input.SemanticSourceMetadata{}
	body, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, err
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, body)
	if err != nil {
		return empty, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	entries := []v2input.Entry{{Ref: metadata.Inputs.Pixels, Contract: "WorkingPixels"}}
	if metadata.Inputs.Alpha != nil {
		entries = append(entries, v2input.Entry{Ref: *metadata.Inputs.Alpha, Contract: "WorkingAlpha"})
	}
	for _, entry := range entries {
		node := v2draft.LayerRef{Ref: entry.Ref, Contract: entry.Contract, ParentRef: binding.Source}
		object, err := catalog.Lookup(ctx, who, tenantID, node)
		if err != nil {
			return empty, err
		}
		if object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream" {
			return empty, v2input.ErrInvalid
		}
		stream, err := s.Objects.Open(ctx, object)
		if err != nil {
			return empty, err
		}
		var verifyErr error
		if entry.Contract == "WorkingAlpha" {
			verifyErr = masknpy.VerifyAlphaNPY(ctx, stream, object.Bytes, entry.Ref.SHA256, metadata.Width, metadata.Height)
		} else {
			verifyErr = masknpy.VerifyFloat32ArrayNPY(ctx, stream, object.Bytes, entry.Ref.SHA256, metadata.Width, metadata.Height, 3, unitRGB)
		}
		closeErr := stream.Close()
		if verifyErr != nil {
			return empty, verifyErr
		}
		if closeErr != nil {
			return empty, closeErr
		}
		current, err := catalog.Lookup(ctx, who, tenantID, node)
		if err != nil {
			return empty, err
		}
		if current != object {
			return empty, v2input.ErrConflict
		}
	}

	geometry, err := v2input.ResolveSemanticGeometryArrays(binding, body)
	if err != nil {
		return empty, err
	}
	for _, field := range geometry {
		node := v2draft.LayerRef{Ref: field.Ref, Contract: "DenseWarpField", ParentRef: binding.Source}
		object, err := catalog.Lookup(ctx, who, tenantID, node)
		if err != nil {
			return empty, err
		}
		if object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream" {
			return empty, v2input.ErrInvalid
		}
		stream, err := s.Objects.Open(ctx, object)
		if err != nil {
			return empty, err
		}
		verifyErr := masknpy.VerifyFloat32ArrayNPY(ctx, stream, object.Bytes, field.Ref.SHA256, field.Width, field.Height, 2, false)
		closeErr := stream.Close()
		if verifyErr != nil {
			return empty, verifyErr
		}
		if closeErr != nil {
			return empty, closeErr
		}
		current, err := catalog.Lookup(ctx, who, tenantID, node)
		if err != nil {
			return empty, err
		}
		if current != object {
			return empty, v2input.ErrConflict
		}
	}
	// Re-check the exact source pin and authority after all child reads.
	if _, err := s.Read(ctx, who, tenantID, binding); err != nil {
		return empty, err
	}
	return metadata, nil
}

// VerifyQualifiedSourcePixels additionally requires a server-verified ICC
// registry pin; source SHA pins prevent switching documents between reads.
func (s SQLTransferSourceReader) VerifyQualifiedSourcePixels(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile) (v2input.SemanticSourceMetadata, error) {
	return s.verifyQualifiedSourcePixels(ctx, who, tenantID, binding, color, false)
}

// VerifyQualifiedFaceSourcePixels composes the deployed ICC registry pin with
// the face worker unit RGB contract, preserving the general HDR source reader.
func (s SQLTransferSourceReader) VerifyQualifiedFaceSourcePixels(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile) (v2input.SemanticSourceMetadata, error) {
	return s.verifyQualifiedSourcePixels(ctx, who, tenantID, binding, color, true)
}

func (s SQLTransferSourceReader) verifyQualifiedSourcePixels(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, unitRGB bool) (v2input.SemanticSourceMetadata, error) {
	if err := ctx.Err(); err != nil {
		return v2input.SemanticSourceMetadata{}, err
	}
	if color.sha == "" {
		return v2input.SemanticSourceMetadata{}, v2input.ErrInvalid
	}
	body, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return v2input.SemanticSourceMetadata{}, err
	}
	if err := color.VerifyWorkingImage(body); err != nil {
		return v2input.SemanticSourceMetadata{}, err
	}
	return s.verifySourcePixels(ctx, who, tenantID, binding, unitRGB)
}
