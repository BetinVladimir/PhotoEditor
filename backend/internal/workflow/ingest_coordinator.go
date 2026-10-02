package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

type IngestDocumentSource interface {
	Publish(context.Context, uuid.UUID, IngestSpec) ([]PublishedIngestNode, error)
}
type IngestTaskWriter interface {
	Materialize(context.Context, uuid.UUID, uuid.UUID, PreparedTask, *uuid.UUID) (bool, error)
	SealInspection(context.Context, uuid.UUID, uuid.UUID) (bool, error)
}
type IngestInspectionCoordinator struct {
	Documents IngestDocumentSource
	Tasks     IngestTaskWriter
}

// PrepareInspection assembles the initial stage of an unsealed ingest run.
// Resume before the seal reuses all immutable rows/documents. After seal, callers
// must reconcile persisted state instead of recompiling or unsealing the run.
func (c IngestInspectionCoordinator) PrepareInspection(ctx context.Context, tenant uuid.UUID, spec IngestSpec) error {
	if c.Documents == nil || c.Tasks == nil || tenant == uuid.Nil {
		return errors.New("invalid ingest coordinator")
	}
	registry, err := LoadRegistry()
	if err != nil {
		return err
	}
	expected, err := CompileIngest(spec, registry)
	if err != nil {
		return err
	}
	published, err := c.Documents.Publish(ctx, tenant, spec)
	if err != nil {
		return err
	}
	if len(published) != len(expected.Nodes) {
		return errors.New("incomplete ingest document set")
	}
	// Verify all returned documents before writing even the first task.
	prepared := make([]PreparedTask, 0, len(spec.Items)*2)
	for i, entry := range published {
		node := expected.Nodes[i]
		if entry.Node.ID != node.ID || entry.Node.Key != node.Key || entry.Node.Operation != node.Operation {
			return errors.New("ingest document node mismatch")
		}
		hash := sha256.Sum256(node.Params)
		if !validExpansionRef(entry.Params) || entry.Params.SHA256 != hex.EncodeToString(hash[:]) {
			return errors.New("ingest document hash mismatch")
		}
		if node.Operation == "asset.decode" {
			continue
		}
		var params ingestSourceParams
		if json.Unmarshal(node.Params, &params) != nil {
			return errors.New("invalid compiled ingest params")
		}
		task, err := PrepareIngestTask(node, entry.Params, params.Bytes, nil)
		if err != nil {
			return err
		}
		prepared = append(prepared, task)
	}
	for _, task := range prepared {
		if err = ctx.Err(); err != nil {
			return err
		}
		saved, err := c.Tasks.Materialize(ctx, tenant, spec.RunID, task, nil)
		if err != nil {
			return err
		}
		if !saved {
			return errors.New("ingest task materialization conflict")
		}
	}
	sealed, err := c.Tasks.SealInspection(ctx, tenant, spec.RunID)
	if err != nil {
		return err
	}
	if !sealed {
		return errors.New("ingest inspection barrier incomplete")
	}
	return nil
}
