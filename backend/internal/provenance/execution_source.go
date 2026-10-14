package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type SigningTaskCatalog interface {
	ReadCanonicalSigningTask(context.Context, auth.Identity, ReadySigningExport, uuid.UUID) (CanonicalSigningTask, error)
}

// ResolvedSigningTask describes content ancestry, rather than scheduler gating
// edges. The resolver must account for all operation-specific content inputs,
// including references nested in Params/WorkingImage, and prove generated,
// model and review claims against adopted immutable documents. Sources must be
// original encoded source artifacts, never preset/Params/WorkingImage hashes.
type ResolvedSigningTask struct {
	Node    SigningExecutionNode
	Sources []v2input.Ref
}
type SigningTaskLineageResolver interface {
	ResolveSigningTaskLineage(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (ResolvedSigningTask, error)
}

type BoundSigningExecutionSource struct {
	Tasks  SigningTaskCatalog
	Claims SigningTaskLineageResolver
}

func (s BoundSigningExecutionSource) ReadSigningExecution(ctx context.Context, who auth.Identity, ready ReadySigningExport) (SigningExecutionGraph, error) {
	var zero SigningExecutionGraph
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Tasks == nil || s.Claims == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Task == uuid.Nil || ready.Run == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || !ref(ready.Ref) {
		return zero, ErrInvalid
	}
	graph := SigningExecutionGraph{FinalTask: ready.Task, Export: ready.Ref}
	visited := map[uuid.UUID]bool{}
	sources := map[uuid.UUID]v2input.Ref{}
	queue := []uuid.UUID{ready.Task}
	totalItems, totalBytes := 0, 0
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if visited[id] {
			continue
		}
		visited[id] = true
		if len(visited) > 5000 {
			return zero, ErrInvalid
		}
		evidence, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, id)
		if err != nil {
			return zero, err
		}
		if evidence.Task != id || normalizeCanonicalSigningTask(&evidence) != nil {
			return zero, ErrInvalid
		}
		if id == ready.Task && evidence.Run != ready.Run {
			return zero, ErrInvalid
		}
		proof, err := canonical.Marshal(evidence)
		if err != nil {
			return zero, ErrInvalid
		}
		totalBytes += len(proof)
		if totalBytes > 64<<20 {
			return zero, ErrInvalid
		}
		// Own nested DB buffers before the operation-specific resolver runs.
		var owned CanonicalSigningTask
		if json.Unmarshal(proof, &owned) != nil {
			return zero, ErrInvalid
		}
		resolved, err := s.Claims.ResolveSigningTaskLineage(ctx, who, ready, owned)
		if err != nil {
			return zero, err
		}
		raw, err := canonical.Marshal(resolved)
		if err != nil || len(raw) > 16<<20 {
			return zero, ErrInvalid
		}
		var bound ResolvedSigningTask
		if json.Unmarshal(raw, &bound) != nil {
			return zero, ErrInvalid
		}
		node := bound.Node
		if node.Task != evidence.Task || node.Run != evidence.Run || node.CanonicalResult != evidence.Result || node.Attempt != evidence.Attempt || node.Fence != evidence.Fence || node.Action.Operation != evidence.Operation || node.Action.Version != evidence.Version || node.Action.ParamsSHA != evidence.Params.SHA256 {
			return zero, ErrInvalid
		}
		outputs, err := canonicalTaskOutputRefs(evidence.Outputs)
		if err != nil {
			return zero, err
		}
		if !sameSigningRefs(node.Outputs, outputs) {
			return zero, ErrInvalid
		}
		if _, err := signingNodeContentOutputs(node); err != nil {
			return zero, err
		}
		// Re-read full execution evidence after resolving external claims. A new
		// result/fence, modified Params or changed metrics invalidates the graph.
		current, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, id)
		if err != nil {
			return zero, err
		}
		if normalizeCanonicalSigningTask(&current) != nil {
			return zero, ErrInvalid
		}
		currentProof, err := canonical.Marshal(current)
		if err != nil || !bytes.Equal(proof, currentProof) {
			return zero, ErrInvalid
		}
		totalItems += len(node.Parents) + len(node.Inputs) + len(node.Outputs) + len(node.Children) + len(bound.Sources)
		if totalItems > 100000 {
			return zero, ErrInvalid
		}
		for _, source := range bound.Sources {
			if !ref(source) {
				return zero, ErrInvalid
			}
			if old, ok := sources[source.ArtifactID]; ok && old != source {
				return zero, ErrInvalid
			}
			sources[source.ArtifactID] = source
		}
		if len(sources) > 5000 {
			return zero, ErrInvalid
		}
		graph.Nodes = append(graph.Nodes, node)
		queue = append(queue, node.Parents...)
	}
	for _, source := range sources {
		graph.Sources = append(graph.Sources, source)
	}
	sort.Slice(graph.Sources, func(i, j int) bool {
		return graph.Sources[i].ArtifactID.String() < graph.Sources[j].ArtifactID.String()
	})
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].Task.String() < graph.Nodes[j].Task.String() })
	if _, err := CompileSigningLineage(ctx, graph); err != nil {
		return zero, err
	}
	return graph, nil
}

func canonicalTaskOutputRefs(raw []byte) ([]v2input.Ref, error) {
	var refs []v2input.Ref
	if len(raw) < 2 || len(raw) > 16<<20 || json.Unmarshal(raw, &refs) != nil || len(refs) < 1 || len(refs) > 5000 {
		return nil, ErrInvalid
	}
	encoded, err := canonical.Marshal(refs)
	if err != nil || !bytes.Equal(encoded, raw) {
		return nil, ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, value := range refs {
		if !ref(value) || seen[value.ArtifactID] {
			return nil, ErrInvalid
		}
		seen[value.ArtifactID] = true
	}
	return refs, nil
}
func sameSigningRefs(left, right []v2input.Ref) bool {
	if len(left) != len(right) {
		return false
	}
	set := map[uuid.UUID]v2input.Ref{}
	for _, r := range left {
		if _, exists := set[r.ArtifactID]; exists {
			return false
		}
		set[r.ArtifactID] = r
	}
	for _, r := range right {
		if set[r.ArtifactID] != r {
			return false
		}
	}
	return true
}

var _ SigningExecutionSource = BoundSigningExecutionSource{}
var _ SigningTaskCatalog = SQLSigningTaskCatalog{}
