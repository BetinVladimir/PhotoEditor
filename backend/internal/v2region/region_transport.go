package v2region

import (
	"bytes"
	"context"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type FrozenRegionInputs struct {
	Open        v2input.CursorFactory
	Direct      []v2input.Ref
	InputDigest string
}

// FreezeInputs snapshots the scoped resolver inventory for transport retries.
// It checks required coverage and Params bytes, not SQL authority, object
// versions or completeness of the resolver's style descendants.
func (p FrozenRegionPlan) FreezeInputs(task uuid.UUID, params v2input.Ref, resolved []v2input.Entry) (FrozenRegionInputs, error) {
	if task == uuid.Nil || len(p.body) == 0 || params.ArtifactID == uuid.Nil || params.SHA256 != p.digest || len(resolved) < 4 || len(resolved) > 511 {
		return FrozenRegionInputs{}, ErrRegionInvalid
	}
	entries := append([]v2input.Entry(nil), resolved...)
	entries = append(entries, v2input.Entry{Ref: params, Contract: "Params"})
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	lookup := make(map[uuid.UUID]v2input.Entry, len(entries))
	for _, entry := range entries {
		hash, err := hex.DecodeString(entry.Ref.SHA256)
		if err != nil || len(hash) != 32 || hex.EncodeToString(hash) != entry.Ref.SHA256 || entry.Ref.ArtifactID == uuid.Nil || len(entry.Contract) < 1 || len(entry.Contract) > 120 {
			return FrozenRegionInputs{}, ErrRegionInvalid
		}
		if _, exists := lookup[entry.Ref.ArtifactID]; exists {
			return FrozenRegionInputs{}, ErrRegionInvalid
		}
		lookup[entry.Ref.ArtifactID] = entry
	}
	for _, required := range p.inputs {
		if lookup[required.Ref.ArtifactID] != required {
			return FrozenRegionInputs{}, ErrRegionInvalid
		}
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		PlanSHA   string          `json:"plan_sha256"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, "region.preview", "1", p.digest, entries})
	if err != nil {
		return FrozenRegionInputs{}, err
	}
	return FrozenRegionInputs{Open: func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &regionInputCursor{entries: entries}, nil
	}, Direct: []v2input.Ref{params}, InputDigest: digest}, nil
}

type regionInputCursor struct {
	entries []v2input.Entry
	offset  int
}

func (c *regionInputCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Entry{}, false, err
	}
	if c.offset == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	entry := c.entries[c.offset]
	c.offset++
	return entry, true, nil
}
