package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// DocumentWitness owns JSON bytes actually read and validated during scoped
// closure resolution. It is evidence of that read, not a transferable grant.
// Repeated occurrences that reuse bytes still receive independent catalog scope
// checks, but do not duplicate this read witness. Binary leaves stay in inventory.
type DocumentWitness struct {
	Path     string
	Contract string
	Ref      v2input.Ref
	Parent   v2input.Ref
	Object   objectstore.Object
	JCS      string
}

// OccurrenceWitness records one scoped catalog occurrence, including binary
// leaves and repeated refs. Binary object bytes are not verified by this record.
type OccurrenceWitness struct {
	Node   LayerRef
	Object objectstore.Object
}

type TaskInputEvidence struct {
	Entries     []v2input.Entry
	Documents   []DocumentWitness
	Occurrences []OccurrenceWitness
}

func ownOccurrenceNode(node LayerRef) LayerRef {
	if node.ManualVectors != nil {
		value := *node.ManualVectors
		node.ManualVectors = &value
	}
	if node.ExpectedBinding != nil {
		v := *node.ExpectedBinding
		node.ExpectedBinding = &v
	}
	if node.ExpectedLearning != nil {
		v := *node.ExpectedLearning
		node.ExpectedLearning = &v
	}
	if node.RenderFrame != nil {
		v := *node.RenderFrame
		node.RenderFrame = &v
	}
	if node.ExpectedBackground != nil {
		v := *node.ExpectedBackground
		if v.AssumeProfile != nil {
			value := *v.AssumeProfile
			v.AssumeProfile = &value
		}
		node.ExpectedBackground = &v
	}
	return node
}

// ResolveTaskInputEvidence performs one traversal and returns owned read and
// occurrence evidence only when all schema/geometry/scope checks succeed.
func (s ClosureResolver) ResolveTaskInputEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID, roots []v2input.Entry, binding v2input.SessionVersionBinding) (TaskInputEvidence, error) {
	result := TaskInputEvidence{Documents: []DocumentWitness{}, Occurrences: []OccurrenceWitness{}}
	entries, err := s.resolveTaskInputs(ctx, who, tenant, roots, binding, &result.Documents, &result.Occurrences)
	if err != nil {
		return TaskInputEvidence{}, err
	}
	result.Entries = entries
	return result, nil
}

// ResolveTaskInputsWithWitnesses returns the same inventory as ResolveTaskInputs
// and owned JSON read evidence in traversal order, bounded to 32 MiB aggregate.
// No partial inventory or witnesses escape a failed traversal/schema/scope check.
func (s ClosureResolver) ResolveTaskInputsWithWitnesses(ctx context.Context, who auth.Identity, tenant uuid.UUID, roots []v2input.Entry, binding v2input.SessionVersionBinding) ([]v2input.Entry, []DocumentWitness, error) {
	witnesses := []DocumentWitness{}
	entries, err := s.resolveTaskInputs(ctx, who, tenant, roots, binding, &witnesses)
	if err != nil {
		return nil, nil, err
	}
	return entries, witnesses, nil
}
