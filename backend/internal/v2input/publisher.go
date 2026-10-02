package v2input

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
)

type DocumentPublisher interface {
	Publish(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error)
}

// ScopedPublisher uses the existing verified backend JSON publisher. Each
// ordinal has a separate durable reservation, so retries cannot overwrite a
// prior chunk; the V2 SQL functions authenticate task/run scope.
type ScopedPublisher struct {
	Documents DocumentPublisher
	Tenant    uuid.UUID
	Run       uuid.UUID
}

func (p ScopedPublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (Ref, error) {
	if p.Documents == nil || p.Tenant == uuid.Nil || p.Run == uuid.Nil || task == uuid.Nil {
		return Ref{}, ErrInvalid
	}
	var purpose string
	switch contract {
	case "InputClosureChunk":
		if ordinal < 0 || ordinal >= MaxChunks {
			return Ref{}, ErrInvalid
		}
		purpose = fmt.Sprintf("v2_input_chunk_%d", ordinal)
	case "InputClosureManifest":
		if ordinal < 1 || ordinal > MaxChunks {
			return Ref{}, ErrInvalid
		}
		purpose = "v2_input_manifest"
	default:
		return Ref{}, ErrInvalid
	}
	ref, err := p.Documents.Publish(ctx, artifactjson.Scope{
		Tenant: p.Tenant, Run: p.Run, Owner: task, Purpose: purpose,
	}, body)
	if err != nil {
		return Ref{}, err
	}
	return Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, nil
}
