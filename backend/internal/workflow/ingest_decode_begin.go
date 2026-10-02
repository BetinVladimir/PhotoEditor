package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"sort"
	"strconv"
	"strings"
)

func (r IngestPlanRepository) BeginDecode(ctx context.Context, tenant, run uuid.UUID, plan IngestDecodePlan) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil || !plan.Ready || len(plan.Tasks)+len(plan.Failures) < 1 || len(plan.Tasks)+len(plan.Failures) > 10000 {
		return false, errors.New("invalid decode phase plan")
	}
	type metadata struct {
		Ordinal int64 `json:"ordinal"`
		review.ArtifactRef
	}
	refs := []metadata{}
	seen := map[int64]bool{}
	for _, node := range plan.Tasks {
		ordinal, err := strconv.ParseInt(strings.TrimPrefix(node.Task.NodeKey, "asset.decode/"), 10, 64)
		if err != nil || !strings.HasPrefix(node.Task.NodeKey, "asset.decode/") || ordinal < 0 || ordinal > 9007199254740991 || seen[ordinal] || node.Task.Operation != "asset.decode" || !validExpansionRef(node.Metadata) {
			return false, errors.New("invalid decode metadata manifest")
		}
		seen[ordinal] = true
		refs = append(refs, metadata{ordinal, node.Metadata})
	}
	for _, failure := range plan.Failures {
		if failure.Ordinal < 0 || failure.Ordinal > 9007199254740991 || seen[failure.Ordinal] {
			return false, errors.New("invalid failed ingest ordinal")
		}
		seen[failure.Ordinal] = true
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Ordinal < refs[j].Ordinal })
	raw, err := canonical.Marshal(refs)
	if err != nil {
		return false, err
	}
	var started bool
	err = r.DB.QueryRow(ctx, "SELECT photo.begin_ingest_decode($1,$2,$3)", tenant, run, raw).Scan(&started)
	return started, err
}
