package provenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Graph nodes must originate from canonical succeeded task evidence, including
// adopted operation claims. This compiler checks graph consistency, not SQL ACL,
// canonical-result fencing or the truth of an operator's generation claims.
// Child outputs are physical adopted ImageResult children, never extra declared
// task outputs. The operation resolver must prove exact root bytes and adoption.
type SigningChildOutput struct {
	Root v2input.Ref
	Ref  v2input.Ref
	Role string
}
type SigningExecutionNode struct {
	Task            uuid.UUID
	Run             uuid.UUID
	CanonicalResult uuid.UUID
	Attempt         int
	Fence           int64
	Parents         []uuid.UUID
	Inputs          []v2input.Ref
	Outputs         []v2input.Ref
	Children        []SigningChildOutput
	Action          SigningAction
}
type SigningExecutionGraph struct {
	FinalTask uuid.UUID
	Export    v2input.Ref
	Sources   []v2input.Ref
	Nodes     []SigningExecutionNode
}

func CompileSigningLineage(ctx context.Context, graph SigningExecutionGraph) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if graph.FinalTask == uuid.Nil || !ref(graph.Export) || len(graph.Sources) < 1 || len(graph.Sources) > 5000 || len(graph.Nodes) < 1 || len(graph.Nodes) > 5000 {
		return nil, ErrInvalid
	}
	sources := map[uuid.UUID]v2input.Ref{}
	sourceHashes := map[string]bool{}
	for _, source := range graph.Sources {
		if !ref(source) {
			return nil, ErrInvalid
		}
		if _, ok := sources[source.ArtifactID]; ok {
			return nil, ErrInvalid
		}
		sources[source.ArtifactID] = source
		sourceHashes[source.SHA256] = true
	}
	nodes := map[uuid.UUID]SigningExecutionNode{}
	results := map[uuid.UUID]bool{}
	producers := map[uuid.UUID]uuid.UUID{}
	outputs := map[uuid.UUID]v2input.Ref{}
	edges := 0
	for _, node := range graph.Nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if node.Task == uuid.Nil || node.Run == uuid.Nil || node.CanonicalResult == uuid.Nil || node.Attempt < 1 || node.Fence < 1 || len(node.Outputs) < 1 || len(node.Outputs) > 5000 || len(node.Inputs) > 30000 {
			return nil, ErrInvalid
		}
		if _, exists := nodes[node.Task]; exists || results[node.CanonicalResult] {
			return nil, ErrInvalid
		}
		nodes[node.Task] = node
		results[node.CanonicalResult] = true
		contentOutputs, err := signingNodeContentOutputs(node)
		if err != nil {
			return nil, err
		}
		edges += len(node.Parents) + len(node.Inputs) + len(contentOutputs)
		if edges > 100000 {
			return nil, ErrInvalid
		}
		for _, output := range contentOutputs {
			if !ref(output) {
				return nil, ErrInvalid
			}
			if _, exists := outputs[output.ArtifactID]; exists {
				return nil, ErrInvalid
			}
			if _, exists := sources[output.ArtifactID]; exists {
				return nil, ErrInvalid
			}
			outputs[output.ArtifactID] = output
			producers[output.ArtifactID] = node.Task
		}
	}
	if outputs[graph.Export.ArtifactID] != graph.Export || producers[graph.Export.ArtifactID] != graph.FinalTask {
		return nil, ErrInvalid
	}
	usedSources := map[uuid.UUID]bool{}
	for _, node := range graph.Nodes {
		parents := map[uuid.UUID]bool{}
		for _, parent := range node.Parents {
			if parent == node.Task || parents[parent] {
				return nil, ErrInvalid
			}
			if _, exists := nodes[parent]; !exists {
				return nil, ErrInvalid
			}
			parents[parent] = true
		}
		usedParents := map[uuid.UUID]bool{}
		seenInputs := map[uuid.UUID]bool{}
		for _, input := range node.Inputs {
			if !ref(input) || seenInputs[input.ArtifactID] {
				return nil, ErrInvalid
			}
			seenInputs[input.ArtifactID] = true
			if source, exists := sources[input.ArtifactID]; exists {
				if source != input {
					return nil, ErrInvalid
				}
				usedSources[input.ArtifactID] = true
				continue
			}
			if outputs[input.ArtifactID] != input || !parents[producers[input.ArtifactID]] {
				return nil, ErrInvalid
			}
			usedParents[producers[input.ArtifactID]] = true
		}
		if len(usedParents) != len(parents) {
			return nil, ErrInvalid
		}
	}
	if len(usedSources) != len(sources) {
		return nil, ErrInvalid
	}
	state := map[uuid.UUID]int{}
	ordered := []SigningAction{}
	var visit func(uuid.UUID) error
	visit = func(id uuid.UUID) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if state[id] == 1 {
			return ErrInvalid
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		node := nodes[id]
		parents := append([]uuid.UUID(nil), node.Parents...)
		sort.Slice(parents, func(i, j int) bool { return parents[i].String() < parents[j].String() })
		for _, parent := range parents {
			if err := visit(parent); err != nil {
				return err
			}
		}
		state[id] = 2
		ordered = append(ordered, node.Action)
		return nil
	}
	if err := visit(graph.FinalTask); err != nil {
		return nil, err
	}
	if len(state) != len(nodes) {
		return nil, ErrInvalid
	}
	hashes := make([]string, 0, len(sourceHashes))
	for hash := range sourceHashes {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	body, err := canonical.Marshal(SigningLineage{Schema: 1, SourceHashes: hashes, Actions: ordered})
	if err != nil {
		return nil, err
	}
	// Reuse the full contract validation without publishing an artifact or
	// treating the local synthetic UUID as any kind of authority.
	reference := v2input.Ref{ArtifactID: graph.FinalTask, SHA256: bodyDigest(body)}
	if _, err := DecodeSigningLineage(body, reference); err != nil {
		return nil, err
	}
	return body, nil
}

func bodyDigest(raw []byte) string { hash := sha256.Sum256(raw); return hex.EncodeToString(hash[:]) }

func signingNodeContentOutputs(node SigningExecutionNode) ([]v2input.Ref, error) {
	outputs := append([]v2input.Ref(nil), node.Outputs...)
	if len(node.Children) == 0 {
		return outputs, nil
	}
	if (node.Action.Operation != "render.compose" && node.Action.Operation != "optical.negative") || node.Action.Version != "1" || len(node.Outputs) != 1 || len(node.Children) > 4 || !ref(node.Outputs[0]) {
		return nil, ErrInvalid
	}
	roles := map[string]bool{}
	ids := map[uuid.UUID]bool{node.Outputs[0].ArtifactID: true}
	for _, child := range node.Children {
		switch child.Role {
		case "working_image", "changed_mask", "report":
		case "generated_mask":
			if node.Action.Operation == "optical.negative" { return nil, ErrInvalid }
		default:
			return nil, ErrInvalid
		}
		if roles[child.Role] || child.Root != node.Outputs[0] || !ref(child.Ref) || ids[child.Ref.ArtifactID] {
			return nil, ErrInvalid
		}
		roles[child.Role] = true
		ids[child.Ref.ArtifactID] = true
		outputs = append(outputs, child.Ref)
	}
	if !roles["working_image"] || !roles["changed_mask"] || !roles["report"] {
		return nil, ErrInvalid
	}
	return outputs, nil
}
