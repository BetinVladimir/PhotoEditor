package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingFrameDescriptorReader interface {
	ReadFrameDescriptor(context.Context, auth.Identity, uuid.UUID, uuid.UUID,
		v2input.SessionVersionBinding, string,
		*v2draft.ResolvedCorrection) (v2draft.PinnedFrameDescriptor, error)
}

type TrainingDescriptor struct {
	Features   [12]float64
	Faces      v2input.Ref
	Shard      v2input.Ref
	AnalysisID uuid.UUID
	Ordinal    int
}

type TrainingDescriptorSource struct {
	Reader TrainingFrameDescriptorReader
}

// Load selects the adopted pre-edit descriptor for the exact asset/source
// binding. A session correction, when present, pins the originating analysis.
func (s TrainingDescriptorSource) Load(ctx context.Context,
	who auth.Identity, tenantID, projectID uuid.UUID,
	binding v2input.SessionVersionBinding, modelLock string,
	session *v2draft.ResolvedCorrection) (TrainingDescriptor, error) {
	if s.Reader == nil || tenantID == uuid.Nil || projectID == uuid.Nil ||
		binding.AssetVersionID == uuid.Nil ||
		!validManifestRef(binding.Source) || !validSHA(binding.CanvasHash) ||
		!validSHA(modelLock) {
		return TrainingDescriptor{}, ErrInvalidTrainingDraft
	}
	pinned, err := s.Reader.ReadFrameDescriptor(ctx, who, tenantID,
		projectID, binding, modelLock, session)
	if err != nil {
		return TrainingDescriptor{}, err
	}
	if pinned.Features.Binding != binding ||
		!validManifestRef(pinned.Features.Faces) ||
		!validManifestRef(pinned.ShardRef) ||
		pinned.AnalysisID == uuid.Nil || pinned.Ordinal < 0 ||
		pinned.Ordinal > 4999 {
		return TrainingDescriptor{}, ErrInvalidTrainingDraft
	}
	features, err := v2draft.Scene12Features(pinned.Features)
	if err != nil {
		return TrainingDescriptor{}, err
	}
	return TrainingDescriptor{Features: features, Faces: pinned.Features.Faces,
		Shard: pinned.ShardRef, AnalysisID: pinned.AnalysisID,
		Ordinal: pinned.Ordinal}, nil
}

var _ TrainingFrameDescriptorReader = v2draft.Service{}
