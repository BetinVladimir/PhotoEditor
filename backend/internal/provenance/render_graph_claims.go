package provenance

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type SigningRenderDependencyClaim struct {
	Entry v2input.Entry
	// parameter binds non-pixel metadata to Params; source is a physically
	// qualified encoded background; producer contributes a content graph edge.
	Kind     string
	Producer uuid.UUID
}
type SigningRenderDependencyResolver interface {
	ResolveSigningRenderDependencies(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, SigningRenderCausalInventory) ([]SigningRenderDependencyClaim, error)
}
type signingRenderReviewedReader interface {
	read(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (signingRenderReviewedCausalReceipt, error)
}
type RenderSigningTaskResolver struct {
	Reviewed     signingRenderReviewedReader
	Dependencies SigningRenderDependencyResolver
}

func compileSigningRenderGraphTask(task CanonicalSigningTask, receipt signingRenderReviewedCausalReceipt, claims []SigningRenderDependencyClaim) (ResolvedSigningTask, error) {
	var zero ResolvedSigningTask
	if normalizeCanonicalSigningTask(&task) != nil || task.Operation != "render.compose" || task.Version != "1" {
		return zero, ErrInvalid
	}
	hash, err := canonical.Hash(task)
	if err != nil || hash != receipt.causal.inventory.taskDigest || !digest(receipt.causal.stamp) || !digest(receipt.causal.renderDigest) {
		return zero, ErrInvalid
	}
	inventory := receipt.causal.inventory
	pixelInventory := map[uuid.UUID]v2input.Ref{}
	if len(inventory.Images) == 0 {
		return zero, ErrInvalid
	}
	for _, pin := range append(append([]v2input.Ref(nil), inventory.Images...), inventory.Results...) {
		if !ref(pin) {
			return zero, ErrInvalid
		}
		if _, exists := pixelInventory[pin.ArtifactID]; exists {
			return zero, ErrInvalid
		}
		pixelInventory[pin.ArtifactID] = pin
	}

	expected := map[uuid.UUID]v2input.Entry{}
	add := func(entry v2input.Entry) bool {
		if !ref(entry.Ref) || pixelInventory[entry.Ref.ArtifactID].ArtifactID != uuid.Nil {
			return false
		}
		if _, exists := expected[entry.Ref.ArtifactID]; exists {
			return false
		}
		expected[entry.Ref.ArtifactID] = entry
		return true
	}
	for _, pin := range inventory.Models {
		if !add(v2input.Entry{Ref: pin, Contract: "CandidateProfile"}) {
			return zero, ErrInvalid
		}
	}
	for _, pin := range inventory.Backgrounds {
		if !add(v2input.Entry{Ref: pin, Contract: "StyleBackgroundAsset"}) {
			return zero, ErrInvalid
		}
	}
	for _, entry := range inventory.Controls {
		if !add(entry) {
			return zero, ErrInvalid
		}
	}
	if len(claims) != len(expected) || len(expected)+len(pixelInventory) > 4096 {
		return zero, ErrInvalid
	}
	parents := map[uuid.UUID]bool{}
	inputs := append([]v2input.Ref(nil), receipt.causal.pixels.inputs...)
	if len(inputs) != len(pixelInventory) || len(inputs) != len(receipt.causal.pixels.producers) {
		return zero, ErrInvalid
	}
	pins := map[uuid.UUID]bool{}
	for i, pin := range inputs {
		producer := receipt.causal.pixels.producers[i]
		if pixelInventory[pin.ArtifactID] != pin || pins[pin.ArtifactID] || producer == uuid.Nil || producer == task.Task {
			return zero, ErrInvalid
		}
		pins[pin.ArtifactID] = true
		parents[producer] = true
	}
	sources := []v2input.Ref{}
	covered := map[uuid.UUID]bool{}
	for _, claim := range claims {
		id := claim.Entry.Ref.ArtifactID
		entry, exists := expected[id]
		if !exists || entry != claim.Entry || covered[id] {
			return zero, ErrInvalid
		}
		covered[id] = true
		switch claim.Kind {
		case "parameter":
			if claim.Producer != uuid.Nil || claim.Entry.Contract == "StyleBackgroundAsset" {
				return zero, ErrInvalid
			}
		case "source":
			if claim.Producer != uuid.Nil || claim.Entry.Contract != "StyleBackgroundAsset" || pins[id] {
				return zero, ErrInvalid
			}
			sources = append(sources, claim.Entry.Ref)
			inputs = append(inputs, claim.Entry.Ref)
			pins[id] = true
		case "producer":
			if claim.Producer == uuid.Nil || claim.Producer == task.Task || pins[id] {
				return zero, ErrInvalid
			}
			parents[claim.Producer] = true
			inputs = append(inputs, claim.Entry.Ref)
			pins[id] = true
		default:
			return zero, ErrInvalid
		}
	}
	outputs, err := canonicalTaskOutputRefs(task.Outputs)
	if err != nil {
		return zero, err
	}
	action := receipt.outputs.Action
	if action.ModelLockSHA == nil || !digest(*action.ModelLockSHA) || (task.ReviewRequired && action.ReviewID == nil) || action.Operation != task.Operation || action.Version != task.Version || action.ParamsSHA != task.Params.SHA256 {
		return zero, ErrInvalid
	}
	ordered := make([]uuid.UUID, 0, len(parents))
	for parent := range parents {
		ordered = append(ordered, parent)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].ArtifactID.String() < inputs[j].ArtifactID.String() })
	sort.Slice(sources, func(i, j int) bool { return sources[i].ArtifactID.String() < sources[j].ArtifactID.String() })
	result := ResolvedSigningTask{Node: SigningExecutionNode{Task: task.Task, Run: task.Run, CanonicalResult: task.Result, Attempt: task.Attempt, Fence: task.Fence, Parents: ordered, Inputs: inputs, Outputs: outputs, Children: receipt.outputs.Children, Action: action}, Sources: sources}
	contentOutputs, err := signingNodeContentOutputs(result.Node)
	if err != nil {
		return zero, err
	}
	for _, output := range contentOutputs {
		if pins[output.ArtifactID] {
			return zero, ErrInvalid
		}
	}
	raw, err := canonical.Marshal(result)
	if err != nil || len(raw) > 16<<20 {
		return zero, ErrInvalid
	}
	var owned ResolvedSigningTask
	if json.Unmarshal(raw, &owned) != nil {
		return zero, ErrInvalid
	}
	return owned, nil
}

func (s RenderSigningTaskResolver) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (ResolvedSigningTask, error) {
	var zero ResolvedSigningTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Reviewed == nil || s.Dependencies == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil {
		return zero, ErrInvalid
	}
	ctx = newSigningRenderReadContext(ctx)
	owned, err := ownSigningProducerTask(task)
	if err != nil {
		return zero, err
	}
	read := func() (ResolvedSigningTask, string, error) {
		isolated, err := ownSigningProducerTask(owned)
		if err != nil {
			return zero, "", err
		}
		receipt, err := s.Reviewed.read(ctx, who, ready, isolated)
		if err != nil {
			return zero, "", err
		}
		raw, err := canonical.Marshal(receipt.causal.inventory)
		if err != nil {
			return zero, "", err
		}
		var inventory SigningRenderCausalInventory
		if json.Unmarshal(raw, &inventory) != nil {
			return zero, "", ErrInvalid
		}
		inventory.taskDigest = receipt.causal.inventory.taskDigest
		isolated, err = ownSigningProducerTask(owned)
		if err != nil {
			return zero, "", err
		}
		claims, err := s.Dependencies.ResolveSigningRenderDependencies(ctx, who, ready, isolated, inventory)
		if err != nil {
			return zero, "", err
		}
		result, err := compileSigningRenderGraphTask(owned, receipt, claims)
		return result, receipt.causal.stamp, err
	}
	result, stamp, err := read()
	if err != nil {
		return zero, err
	}
	current, currentStamp, err := read()
	if err != nil {
		return zero, err
	}
	before, err := canonical.Hash(result)
	if err != nil {
		return zero, err
	}
	after, err := canonical.Hash(current)
	if err != nil || stamp != currentStamp || before != after {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return current, nil
}

var _ SigningTaskLineageResolver = RenderSigningTaskResolver{}
