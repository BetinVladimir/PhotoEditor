package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/review"
)

type IngestPlanRegistrar interface {
	Register(context.Context, uuid.UUID, IngestSpec) (IngestPlan, error)
}
type PublishedIngestNode struct {
	Node   CompiledIngestNode
	Params review.ArtifactRef
}
type IngestDocumentBuilder struct {
	Plans     IngestPlanRegistrar
	Publisher ContractPublisher
}

// Publish commits every node plan before object IO. An incomplete publication
// returns no complete plan; stable owner scopes make retry safe. No graph seal
// or runnable task is written here.
func (b IngestDocumentBuilder) Publish(ctx context.Context, tenant uuid.UUID, spec IngestSpec) ([]PublishedIngestNode, error) {
	if b.Plans == nil || b.Publisher == nil || tenant == uuid.Nil {
		return nil, errors.New("invalid ingest document builder")
	}
	plan, err := b.Plans.Register(ctx, tenant, spec)
	if err != nil {
		return nil, err
	}
	out := make([]PublishedIngestNode, 0, len(plan.Nodes))
	for _, node := range plan.Nodes {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		ref, err := b.Publisher.Publish(ctx, artifactjson.Scope{Tenant: tenant, Run: spec.RunID, Owner: node.ID, Purpose: "task_params"}, node.Params)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(node.Params)
		result := review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}
		if !validExpansionRef(result) || ref.SHA256 != hex.EncodeToString(hash[:]) {
			return nil, errors.New("ingest params publication integrity failure")
		}
		out = append(out, PublishedIngestNode{Node: node, Params: result})
	}
	return out, nil
}
