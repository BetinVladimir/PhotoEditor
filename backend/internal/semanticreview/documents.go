package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
)

type PlanObjectReader func(context.Context, PlanSource) ([]byte, error)
type PlanSourceLoader func(context.Context) (PlanSources, error)

type PlanDocument struct {
	Source PlanSource
	Body   []byte
}

// ReadCurrentPlanDocuments reauthorizes after reading all objects. The commit
// transaction must still acquire locks and compare this returned inventory.
func ReadCurrentPlanDocuments(ctx context.Context, load PlanSourceLoader, read PlanObjectReader, budgetBytes int64) (PlanSources, []PlanDocument, error) {
	var empty PlanSources
	if load == nil || read == nil {
		return empty, nil, ErrInvalid
	}
	before, err := load(ctx)
	if err != nil {
		return empty, nil, err
	}
	before.Objects = append([]PlanSource(nil), before.Objects...)
	documents, err := ReadPlanDocuments(ctx, before, read, budgetBytes)
	if err != nil {
		return empty, nil, err
	}
	after, err := load(ctx)
	if err != nil {
		return empty, nil, err
	}
	if err = ctx.Err(); err != nil {
		return empty, nil, err
	}
	if !EqualPlanSources(before, after) {
		return empty, nil, ErrConflict
	}
	return before, documents, nil
}

// ReadPlanDocuments performs no database mutation. Callers must reauthorize and
// compare the entire inventory after I/O, before using these bytes to commit.
// The reader must fetch the supplied exact version and bound reads by Bytes.
func ReadPlanDocuments(ctx context.Context, sources PlanSources, read PlanObjectReader, budgetBytes int64) ([]PlanDocument, error) {
	if read == nil || budgetBytes < 1 || budgetBytes > 512<<20 {
		return nil, ErrInvalid
	}
	if err := ValidatePlanSources(sources); err != nil {
		return nil, err
	}
	// Own metadata before invoking external code; it may otherwise alias Objects.
	objects := append([]PlanSource(nil), sources.Objects...)
	var total int64
	for _, source := range objects {
		if source.Bytes > budgetBytes-total {
			return nil, ErrInvalid
		}
		total += source.Bytes
	}
	documents := make([]PlanDocument, 0, len(objects))
	for _, source := range objects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := read(ctx, source)
		if err != nil {
			return nil, err
		}
		if int64(len(body)) != source.Bytes {
			return nil, ErrInvalid
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != source.Ref.SHA256 {
			return nil, ErrInvalid
		}
		// Reject duplicate JSON keys before any typed decoder sees the document.
		if _, err := canonical.Bytes(body); err != nil {
			return nil, ErrInvalid
		}
		documents = append(documents, PlanDocument{Source: source, Body: append([]byte(nil), body...)})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return documents, nil
}

// EqualPlanSources is an exact metadata fence independent of SQL row order.
// Membership, producer generation, revision and physical object versions matter.
func EqualPlanSources(before, after PlanSources) bool {
	if ValidatePlanSources(before) != nil || ValidatePlanSources(after) != nil || before.RunID != after.RunID || before.Revision != after.Revision || before.TaskID != after.TaskID || before.ResultID != after.ResultID || len(before.Objects) != len(after.Objects) {
		return false
	}
	byID := make(map[PlanSource]bool, len(before.Objects))
	for _, source := range before.Objects {
		byID[source] = true
	}
	for _, source := range after.Objects {
		if !byID[source] {
			return false
		}
	}
	return true
}
