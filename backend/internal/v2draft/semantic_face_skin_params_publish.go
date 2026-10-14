package v2draft

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PublishSemanticFaceSkinParams publishes accepted-version server Params after
// verifying the original source/parser/detector metadata chain. Physical label
// content, producer authority and qualification remain admission prerequisites.
func (p Publisher) PublishSemanticFaceSkinParams(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, binding v2input.SessionVersionBinding, image json.RawMessage, labels v2input.Ref, labelsBody json.RawMessage, parserParams v2input.Ref, parserBody json.RawMessage, detection v2input.Ref, detectionBody json.RawMessage) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if !validNativeFaceParamsPublication(who, tenant, document, parserParams) || document == detection.ArtifactID || document == binding.Source.ArtifactID {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	source, err := v2input.ResolveSemanticSourceMetadata(binding, image)
	if err != nil {
		return v2input.Ref{}, err
	}
	if document == source.Inputs.Pixels.ArtifactID || (source.Inputs.Alpha != nil && document == source.Inputs.Alpha.ArtifactID) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	for _, field := range source.Geometry {
		if document == field.ArtifactID {
			return v2input.Ref{}, v2input.ErrInvalid
		}
	}
	prepared, err := v2input.PrepareSemanticFaceSkinParams(ctx, binding, image, labels, labelsBody, parserParams, parserBody, detection, detectionBody)
	if err != nil {
		return v2input.Ref{}, err
	}
	for _, entry := range prepared.Inputs {
		if entry.Ref.ArtifactID == document {
			return v2input.Ref{}, v2input.ErrInvalid
		}
	}
	return p.publish(ctx, who, tenant, binding.AssetVersionID, document, prepared.Params, false, true)
}
