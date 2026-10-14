package interchange

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type ImportReviewWitness struct {
	Entry    uuid.UUID    `json:"entry_id"`
	Target   uuid.UUID    `json:"target_version_id"`
	Kind     string       `json:"kind"`
	Proposal v2input.Ref  `json:"proposal"`
	Base     v2input.Ref  `json:"base"`
	LabelMap *v2input.Ref `json:"label_map,omitempty"`
	Epoch    int64        `json:"epoch"`
	Decision string       `json:"decision"`
	Accepted []string     `json:"accepted_candidate_ids"`
	Rejected []string     `json:"rejected_candidate_ids"`
}

type ImportReviewWitnesses struct {
	mu    sync.RWMutex
	items map[string]ImportReviewWitness
}

func importReviewWitnessKey(w ImportReviewWitness) string {
	return w.Entry.String() + ":" + w.Target.String() + ":" + w.Kind + ":" + w.Proposal.ArtifactID.String()
}

func (r *ImportReviewWitnesses) Record(ctx context.Context, w ImportReviewWitness) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || w.Entry == uuid.Nil || w.Target == uuid.Nil || (w.Kind != "defects" && w.Kind != "dust") || !validImportRef(w.Proposal) || !validImportRef(w.Base) ||
		(w.Kind == "defects" && (w.LabelMap == nil || !validImportRef(*w.LabelMap))) || (w.Kind == "dust" && w.LabelMap != nil) ||
		w.Epoch < 1 || (w.Decision != "accept" && w.Decision != "reject" && w.Decision != "skip") || !validLiveReviewIDs(w.Accepted, w.Rejected) ||
		(w.Decision != "accept" && (len(w.Accepted) != 0 || len(w.Rejected) != 0)) {
		return v2input.ErrInvalid
	}
	w.Accepted = append([]string{}, w.Accepted...)
	w.Rejected = append([]string{}, w.Rejected...)
	if w.LabelMap != nil {
		label := *w.LabelMap
		w.LabelMap = &label
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items == nil {
		r.items = map[string]ImportReviewWitness{}
	}
	key := importReviewWitnessKey(w)
	if old, ok := r.items[key]; ok && (old.Proposal != w.Proposal || old.Base != w.Base || old.Epoch != w.Epoch || old.Decision != w.Decision || !equalImportReviewWitnessSlices(old.Accepted, w.Accepted) || !equalImportReviewWitnessSlices(old.Rejected, w.Rejected) ||
		(old.LabelMap == nil) != (w.LabelMap == nil) || (old.LabelMap != nil && *old.LabelMap != *w.LabelMap)) {
		return v2input.ErrInvalid
	}
	if len(r.items) >= 500 && r.items[key].Entry == uuid.Nil {
		return v2input.ErrInvalid
	}
	r.items[key] = w
	return nil
}

func equalImportReviewWitnessSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *ImportReviewWitnesses) Snapshot(ctx context.Context) ([]ImportReviewWitness, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, v2input.ErrInvalid
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]ImportReviewWitness, 0, len(r.items))
	for _, w := range r.items {
		w.Accepted = append([]string{}, w.Accepted...)
		w.Rejected = append([]string{}, w.Rejected...)
		if w.LabelMap != nil {
			label := *w.LabelMap
			w.LabelMap = &label
		}
		items = append(items, w)
	}
	sort.Slice(items, func(i, j int) bool { return importReviewWitnessKey(items[i]) < importReviewWitnessKey(items[j]) })
	return items, ctx.Err()
}
