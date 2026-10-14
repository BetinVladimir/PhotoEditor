package provenance

import (
	"context"
	"photoeditor/backend/internal/auth"
)

// PresentationSigningTaskResolver is the explicit supported operation set for
// export presentation. Other imaging operations need their own adopted-claims
// resolvers; absence is an error, never an omitted action or generated=false.
type PresentationSigningTaskResolver struct {
	Documents  SigningTaskDocumentReader
	Producers  SigningImageProducer
	Originals  SigningOriginalQualifier
	WorkICCSHA string
}

func (s PresentationSigningTaskResolver) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask) (ResolvedSigningTask, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedSigningTask{}, err
	}
	if e.Version != "1" {
		return ResolvedSigningTask{}, ErrInvalid
	}
	switch e.Operation {
	case "asset.decode":
		return (DecodeSigningTaskResolver{Documents: s.Documents, Originals: s.Originals, WorkICCSHA: s.WorkICCSHA}).ResolveSigningTaskLineage(ctx, who, ready, e)
	case "export.encode":
		return (EncodeSigningTaskResolver{Documents: s.Documents, Producers: s.Producers}).ResolveSigningTaskLineage(ctx, who, ready, e)
	case "metadata.write":
		return (MetadataSigningTaskResolver{Documents: s.Documents, Producers: s.Producers}).ResolveSigningTaskLineage(ctx, who, ready, e)
	case "derivative.sharpen":
		return (SharpenSigningTaskResolver{Documents: s.Documents, Producers: s.Producers}).ResolveSigningTaskLineage(ctx, who, ready, e)
	case "derivative.resize":
		return (ResizeSigningTaskResolver{Documents: s.Documents, Producers: s.Producers}).ResolveSigningTaskLineage(ctx, who, ready, e)
	case "derivative.watermark":
		return (WatermarkSigningTaskResolver{Documents: s.Documents, Producers: s.Producers}).ResolveSigningTaskLineage(ctx, who, ready, e)
	default:
		return ResolvedSigningTask{}, ErrInvalid
	}
}

var _ SigningTaskLineageResolver = PresentationSigningTaskResolver{}
