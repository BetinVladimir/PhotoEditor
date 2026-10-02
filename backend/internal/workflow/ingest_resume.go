package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

type IngestPublishedReader struct{ DB ActivationRows }

// Load reads previously published params without reserving or writing S3 objects.
func (r IngestPublishedReader) Load(ctx context.Context, tenant uuid.UUID, spec IngestSpec) ([]PublishedIngestNode, error) {
	invalid := errors.New("invalid persisted ingest params")
	if r.DB == nil || tenant == uuid.Nil {
		return nil, invalid
	}
	registry, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	plan, err := CompileIngest(spec, registry)
	if err != nil {
		return nil, err
	}
	positions := map[string]int{}
	for i, node := range plan.Nodes {
		positions[node.Key] = i
	}
	rows, err := r.DB.Query(ctx, "SELECT node_id,node_key,operation,params_id,params_sha FROM photo.ingest_published_params($1,$2)", tenant, spec.RunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PublishedIngestNode, len(plan.Nodes))
	seen := map[string]bool{}
	for rows.Next() {
		var id uuid.UUID
		var key, operation string
		var ref review.ArtifactRef
		if err = rows.Scan(&id, &key, &operation, &ref.ID, &ref.SHA256); err != nil {
			return nil, err
		}
		i, ok := positions[key]
		if !ok || seen[key] || !validExpansionRef(ref) {
			return nil, invalid
		}
		node := plan.Nodes[i]
		hash := sha256.Sum256(node.Params)
		if id != node.ID || operation != node.Operation || ref.SHA256 != hex.EncodeToString(hash[:]) {
			return nil, invalid
		}
		seen[key] = true
		out[i] = PublishedIngestNode{Node: node, Params: ref}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(seen) != len(plan.Nodes) {
		return nil, invalid
	}
	return out, nil
}

type IngestSavedDocuments interface {
	Load(context.Context, uuid.UUID, IngestSpec) ([]PublishedIngestNode, error)
}
type IngestSavedOutcomes interface {
	Load(context.Context, uuid.UUID, IngestSpec) ([]InspectionOutcome, error)
}
type IngestDecodeBuilder interface {
	Build(context.Context, uuid.UUID, uuid.UUID, IngestDecodePlan) (bool, error)
}
type IngestDecodeAdvancer struct {
	Documents IngestSavedDocuments
	Outcomes  IngestSavedOutcomes
	Builder   IngestDecodeBuilder
}

func (a IngestDecodeAdvancer) Advance(ctx context.Context, tenant uuid.UUID, spec IngestSpec) (bool, error) {
	if a.Documents == nil || a.Outcomes == nil || a.Builder == nil {
		return false, errors.New("invalid ingest decode advancer")
	}
	docs, err := a.Documents.Load(ctx, tenant, spec)
	if err != nil {
		return false, err
	}
	outcomes, err := a.Outcomes.Load(ctx, tenant, spec)
	if err != nil {
		return false, err
	}
	plan, err := PlanIngestDecode(spec, docs, outcomes)
	if err != nil {
		return false, err
	}
	if !plan.Ready {
		return false, nil
	}
	return a.Builder.Build(ctx, tenant, spec.RunID, plan)
}
