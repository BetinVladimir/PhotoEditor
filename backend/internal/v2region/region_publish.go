package v2region

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type RegionParamsPublisher interface {
	Publish(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, []byte) (v2input.Ref, error)
}

// PublishParams uses the existing immutable draft-upload lifecycle. The stable
// document ID prevents changed plan bytes from overwriting a retry of this task.
// Publication is not graph admission or permission to execute region.preview.
func (p FrozenRegionPlan) PublishParams(ctx context.Context, who auth.Identity, task uuid.UUID, publisher RegionParamsPublisher) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if publisher == nil || task == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || len(p.body) == 0 {
		return v2input.Ref{}, ErrRegionInvalid
	}
	document, err := p.decode()
	if err != nil {
		return v2input.Ref{}, err
	}

	ref, err := publisher.Publish(ctx, who, document.TenantID, document.Request.VersionID, uuid.NewSHA1(task, []byte("region.preview.params:1")), p.JSON())
	if err != nil {
		return v2input.Ref{}, err
	}
	if ref.ArtifactID == uuid.Nil || ref.SHA256 != p.digest {
		return v2input.Ref{}, ErrRegionInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}
