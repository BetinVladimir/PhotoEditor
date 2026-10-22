package semanticreview

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PreviewCommitInputs is an owned SQL projection, not an admitted graph. Final
// materialization must reauthorize source/qualification/quota and ready objects.
type PreviewCommitInputs struct {
	Node      json.RawMessage
	Transport json.RawMessage
	Inventory json.RawMessage
	ParamsJCS json.RawMessage
}

func PrepareExposurePreviewCommitInputs(ctx context.Context, frozen FrozenPreviewTask,
	published PreparedExposureTask) (PreviewCommitInputs, error) {
	empty := PreviewCommitInputs{}
	verified, err := VerifyExposurePreviewPublication(ctx, frozen, published)
	if err != nil {
		return empty, err
	}
	return prepareVerifiedTaskCommitInputs(ctx, verified)
}

// prepareVerifiedTaskCommitInputs accepts only the owned result of a domain
// publication verifier. It never trusts caller-supplied cursors or profiles.
func prepareVerifiedTaskCommitInputs(ctx context.Context, verified PreparedExposureTask) (PreviewCommitInputs, error) {
	empty := PreviewCommitInputs{}
	cursor, err := verified.Graph.Open(ctx)
	if err != nil {
		return empty, err
	}
	entries := make([]v2input.Entry, 0, verified.Graph.Built.Count)
	for {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		entry, more, err := cursor.Next(ctx)
		if err != nil {
			return empty, err
		}
		if !more {
			break
		}
		if len(entries) >= 512 || len(entries) >= len(verified.Graph.Direct) || entry.Ref != verified.Graph.Direct[len(entries)] {
			return empty, ErrInvalid
		}
		entries = append(entries, entry)
	}
	digest, err := canonical.Hash(entries)
	if err != nil || digest != verified.Graph.Built.SHA256 || len(entries) != verified.Graph.Built.Count {
		return empty, ErrInvalid
	}
	node, err := canonical.Marshal(verified.Draft)
	if err != nil {
		return empty, ErrInvalid
	}
	inventory, err := canonical.Marshal(ExposureInventoryBinding{TaskID: verified.Draft.ID, Entries: entries})
	if err != nil {
		return empty, ErrInvalid
	}
	transport, err := canonical.Marshal(ExposureTransportBinding{TaskID: verified.Draft.ID,
		EntryCount: verified.Graph.Built.Count, EntriesSHA: verified.Graph.Built.SHA256,
		Manifest: verified.Graph.Built.Manifest, Chunks: append([]v2input.Ref(nil), verified.Graph.Built.Chunks...),
		DirectRoots: append([]v2input.Ref(nil), verified.Graph.Direct...)})
	if err != nil {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PreviewCommitInputs{Node: node, Transport: transport, Inventory: inventory,
		ParamsJCS: append(json.RawMessage(nil), verified.ParamsJCS...)}, nil
}
