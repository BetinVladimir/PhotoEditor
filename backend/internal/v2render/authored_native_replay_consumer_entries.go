package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// ReadyConsumerEntries resolves physical typed descendants; returned roots are
// never used as a substitute for closure traversal. False means still BLOCKED.
func (s SQLAuthoredNativeReplayService) ReadyConsumerEntries(ctx context.Context, who auth.Identity, tenant, consumer uuid.UUID, base []v2input.Entry) ([]v2input.Entry, bool, error) {
	if tenant == uuid.Nil || consumer == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || len(base) == 0 {
		return nil, false, v2input.ErrInvalid
	}
	base = append([]v2input.Entry(nil), base...)
	read := func() ([]byte, error) {
		var raw []byte
		err := database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
			var role string
			if e := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); e != nil {
				return e
			}
			if role != "photo_scheduler" {
				return v2input.ErrInvalid
			}
			return tx.QueryRow(ctx, "SELECT photo.read_authored_native_replay_consumer_roots($1,$2)", tenant, consumer).Scan(&raw)
		})
		return raw, err
	}
	before, err := read()
	if err != nil {
		return nil, false, err
	}
	if len(before) == 0 || bytes.Equal(before, []byte("null")) {
		return nil, false, nil
	}
	var row struct {
		Roots    []v2input.Entry `json:"roots"`
		Requests []string        `json:"requests"`
	}
	d := json.NewDecoder(bytes.NewReader(before))
	d.DisallowUnknownFields()
	if d.Decode(&row) != nil || len(row.Requests) == 0 || len(row.Requests) > 4999 || len(row.Roots) != 2*len(row.Requests) {
		return nil, false, v2input.ErrInvalid
	}
	byID := map[uuid.UUID]v2input.Entry{}
	used := map[uuid.UUID]bool{}
	merged := map[uuid.UUID]v2input.Entry{}
	for _, entry := range row.Roots {
		if _, ok := byID[entry.Ref.ArtifactID]; ok {
			return nil, false, v2input.ErrInvalid
		}
		byID[entry.Ref.ArtifactID] = entry
	}
	merge := func(entry v2input.Entry) error {
		if old, ok := merged[entry.Ref.ArtifactID]; ok {
			if old.Ref != entry.Ref {
				return v2input.ErrInvalid
			}
			if old.Contract == entry.Contract || entry.Contract == "Artifact" {
				return nil
			}
			if old.Contract != "Artifact" {
				return v2input.ErrInvalid
			}
		}
		merged[entry.Ref.ArtifactID] = entry
		return nil
	}
	for _, raw := range row.Requests {
		request, e := v2input.DecodeAuthoredNativeReplayRequest([]byte(raw))
		if e != nil {
			return nil, false, e
		}
		if request.Consumer.Tenant != tenant || request.Consumer.Task != consumer {
			return nil, false, v2input.ErrInvalid
		}
		run, _, _, requestRef, e := request.IDs()
		if e != nil {
			return nil, false, e
		}
		requestEntry, ok := byID[requestRef.ArtifactID]
		if !ok || requestEntry != (v2input.Entry{Ref: requestRef, Contract: "AuthoredNativeReplayRequest"}) {
			return nil, false, v2input.ErrInvalid
		}
		evidenceEntry, ok := byID[uuid.NewSHA1(run, []byte("evidence"))]
		if !ok || evidenceEntry.Contract != "AuthoredNativeReplayEvidence" {
			return nil, false, v2input.ErrInvalid
		}
		used[requestEntry.Ref.ArtifactID] = true
		used[evidenceEntry.Ref.ArtifactID] = true
		catalog, e := NewScopedAuthoredNativeReplayCatalog(ctx, who, s.Scheduler, s.Objects, s.Bucket, []byte(raw))
		if e != nil {
			return nil, false, e
		}
		anchor, e := catalog.originalBinding(ctx, who, tenant)
		if e != nil {
			return nil, false, e
		}
		roots := append(append([]v2input.Entry(nil), base...), requestEntry, evidenceEntry)
		nodes := []v2draft.LayerRef{{Path: "AuthoredNativeReplay.original_source", Ref: anchor.Source, Contract: "WorkingImage", CanvasHash: anchor.CanvasHash}}
		for _, entry := range roots {
			nodes = append(nodes, v2draft.LayerRef{Path: "AuthoredNativeReplay.consumer", Ref: entry.Ref, Contract: entry.Contract})
		}
		resolver := v2draft.ClosureResolver{Catalog: catalog, Objects: s.Objects}
		actual, e := resolver.ResolveBoundProvenanceEvidence(ctx, who, tenant, nodes, anchor)
		if e != nil {
			return nil, false, e
		}
		for _, required := range roots {
			found := false
			for _, entry := range actual.Entries {
				if entry == required {
					found = true
					break
				}
			}
			if !found {
				return nil, false, v2input.ErrInvalid
			}
		}
		for _, entry := range actual.Entries {
			if e = merge(entry); e != nil {
				return nil, false, e
			}
		}
	}
	if len(used) != len(row.Roots) {
		return nil, false, v2input.ErrInvalid
	}
	after, err := read()
	if err != nil {
		return nil, false, err
	}
	a, e := canonical.Bytes(before)
	b, f := canonical.Bytes(after)
	if e != nil || f != nil || !bytes.Equal(a, b) {
		return nil, false, v2input.ErrConflict
	}
	out := make([]v2input.Entry, 0, len(merged))
	for _, entry := range merged {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Ref.ArtifactID[:], out[j].Ref.ArtifactID[:]) < 0 })
	return out, true, ctx.Err()
}
