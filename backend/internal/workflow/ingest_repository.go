package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"strconv"
	"strings"
)

type IngestPlanRepository struct{ DB artifactjson.RowQuerier }

// Register recompiles the spec, then commits individual immutable plan rows.
// An interrupted batch is intentionally resumable; no task is made executable.
func (r IngestPlanRepository) Register(ctx context.Context, tenant uuid.UUID, spec IngestSpec) (IngestPlan, error) {
	if r.DB == nil || tenant == uuid.Nil {
		return IngestPlan{}, errors.New("invalid ingest plan repository")
	}
	registry, err := LoadRegistry()
	if err != nil {
		return IngestPlan{}, err
	}
	plan, err := CompileIngest(spec, registry)
	if err != nil {
		return IngestPlan{}, err
	}
	for _, node := range plan.Nodes {
		if err = ctx.Err(); err != nil {
			return IngestPlan{}, err
		}
		ordinal, err := strconv.ParseInt(strings.TrimPrefix(node.Key, node.Operation+"/"), 10, 64)
		if err != nil {
			return IngestPlan{}, err
		}
		sum := sha256.Sum256(node.Params)
		var saved bool
		err = r.DB.QueryRow(ctx, "SELECT photo.register_ingest_node($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenant, spec.RunID, node.ID, ordinal, node.Operation, node.Inputs[0].ID, node.Inputs[0].SHA256, hex.EncodeToString(sum[:]), []byte(node.Params)).Scan(&saved)
		if err != nil {
			return IngestPlan{}, err
		}
		if !saved {
			return IngestPlan{}, errors.New("ingest plan stale or conflicting")
		}
	}
	return plan, nil
}
