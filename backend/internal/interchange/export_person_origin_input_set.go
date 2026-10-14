package interchange

import (
	"context"
	"crypto/ed25519"

	"photoeditor/backend/internal/v2input"
)

// ExportPersonOriginCandidateFactory derives entry-scoped, issuer-signed
// attachments from the frozen export and qualified original graph. It is a
// process-installed authority, never a request or archive callback.
type ExportPersonOriginCandidateFactory interface {
	PrepareExportPersonOriginCandidates(context.Context, CreateIntent, FrozenExportDocuments,
		ExportClosureNodeResolver) ([]ExportPersonOriginPublished, error)
}

// ExportPersonOriginInputSet pins prepared claims for the private draft's
// execution, materialization and activation phases. Enrich replays physical
// publication while the graph remains draft and rechecks the current producer.
type ExportPersonOriginInputSet struct {
	candidates []ExportPersonOriginPublished
	publisher  ExportPersonOriginInputPublisher
	trusted    map[PortablePersonOriginKey]ed25519.PublicKey
	current    ExportPersonOriginCurrentChecker
}

func NewExportPersonOriginInputSet(candidates []ExportPersonOriginPublished,
	publisher ExportPersonOriginInputPublisher, trusted map[PortablePersonOriginKey]ed25519.PublicKey,
	current ExportPersonOriginCurrentChecker) (*ExportPersonOriginInputSet, error) {
	if len(candidates) == 0 || len(candidates) > 5000 || publisher == nil || current == nil || len(trusted) == 0 {
		return nil, v2input.ErrInvalid
	}
	owned := &ExportPersonOriginInputSet{publisher: publisher, current: current,
		candidates: make([]ExportPersonOriginPublished, len(candidates)),
		trusted:    make(map[PortablePersonOriginKey]ed25519.PublicKey, len(trusted))}
	for i, candidate := range candidates {
		candidate.Attachment.Body = candidate.Attachment.OwnedBody()
		owned.candidates[i] = candidate
	}
	for key, public := range trusted {
		owned.trusted[key] = append(ed25519.PublicKey(nil), public...)
	}
	return owned, nil
}

func (s *ExportPersonOriginInputSet) Enrich(ctx context.Context, intent CreateIntent,
	frozen FrozenExportDocuments, base ExportInputInventory) (ExportInputInventory, error) {
	if s == nil {
		return ExportInputInventory{}, v2input.ErrInvalid
	}
	candidates := make([]ExportPersonOriginPublished, len(s.candidates))
	for i, candidate := range s.candidates {
		candidate.Attachment.Body = candidate.Attachment.OwnedBody()
		candidates[i] = candidate
	}
	inventory, _, err := PublishExportPersonOriginInputs(ctx, intent, frozen, base,
		candidates, s.publisher, s.trusted, s.current)
	return inventory, err
}
