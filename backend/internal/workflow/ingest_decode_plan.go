package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"

	"photoeditor/backend/internal/review"
)

// InspectionOutcome is loaded from canonical DB task state and hash-verified
// metadata, never from worker event payloads or a client supplied status.
type InspectionOutcome struct {
	Ordinal         int64
	Verify, Inspect TaskState
	InspectReason   string
	MetadataRef     *review.ArtifactRef
	Metadata        *IngestMetadata
}
type IngestDecodeTask struct {
	Task     PreparedTask
	Metadata review.ArtifactRef
}
type IngestItemFailure struct {
	Ordinal       int64
	Source        review.ArtifactRef
	Stage, Reason string
}
type IngestDecodePlan struct {
	Ready    bool
	Tasks    []IngestDecodeTask
	Failures []IngestItemFailure
}

// PlanIngestDecode preserves all snapshot ordinals, including failed files.
// A waiting item holds the entire phase barrier; no partial runnable plan leaks.
func PlanIngestDecode(spec IngestSpec, documents []PublishedIngestNode, outcomes []InspectionOutcome) (IngestDecodePlan, error) {
	fail := func() (IngestDecodePlan, error) {
		return IngestDecodePlan{}, errors.New("invalid ingest inspection outcomes")
	}
	registry, err := LoadRegistry()
	if err != nil {
		return IngestDecodePlan{}, err
	}
	compiled, err := CompileIngest(spec, registry)
	if err != nil {
		return fail()
	}
	if len(documents) != len(compiled.Nodes) || len(outcomes) != len(spec.Items) {
		return fail()
	}
	expectedNodes := map[string]CompiledIngestNode{}
	for _, node := range compiled.Nodes {
		expectedNodes[node.Key] = node
	}
	refs := map[string]review.ArtifactRef{}
	for _, d := range documents {
		expected, known := expectedNodes[d.Node.Key]
		hash := sha256.Sum256(expected.Params)
		if !known || d.Node.ID != expected.ID || d.Node.Operation != expected.Operation || d.Params.SHA256 != hex.EncodeToString(hash[:]) {
			return fail()
		}
		if _, exists := refs[d.Node.Key]; exists || !validExpansionRef(d.Params) {
			return fail()
		}
		refs[d.Node.Key] = d.Params
	}
	byOrdinal := map[int64]InspectionOutcome{}
	for _, o := range outcomes {
		if _, exists := byOrdinal[o.Ordinal]; exists {
			return fail()
		}
		byOrdinal[o.Ordinal] = o
	}
	items := append([]IngestItem(nil), spec.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Ordinal < items[j].Ordinal })
	plan := IngestDecodePlan{Ready: true, Tasks: []IngestDecodeTask{}, Failures: []IngestItemFailure{}}
	waiting := false
	pending := func(s TaskState) bool {
		return s == Blocked || s == Ready || s == Queued || s == Running || s == RetryWait
	}
	for i, item := range items {
		o, exists := byOrdinal[item.Ordinal]
		if !exists {
			return fail()
		}
		if pending(o.Verify) {
			if o.Inspect != Blocked || o.Metadata != nil || o.MetadataRef != nil {
				return fail()
			}
			waiting = true
			continue
		}
		if o.Verify == Failed || o.Verify == Cancelled {
			if o.Metadata != nil || o.MetadataRef != nil {
				return fail()
			}
			if o.Inspect == Blocked {
				waiting = true
				continue
			}
			if o.Inspect != Skipped || o.InspectReason != "upstream_failed" {
				return fail()
			}
			plan.Failures = append(plan.Failures, IngestItemFailure{item.Ordinal, item.Source, "asset.verify", string(o.Verify)})
			continue
		}
		if o.Verify != Succeeded {
			return fail()
		}
		if pending(o.Inspect) {
			if o.Metadata != nil || o.MetadataRef != nil {
				return fail()
			}
			waiting = true
			continue
		}
		if o.Inspect == Failed || o.Inspect == Cancelled {
			if o.Metadata != nil || o.MetadataRef != nil {
				return fail()
			}
			plan.Failures = append(plan.Failures, IngestItemFailure{item.Ordinal, item.Source, "asset.inspect", string(o.Inspect)})
			continue
		}
		if o.Inspect != Succeeded || o.Metadata == nil || o.MetadataRef == nil || !validExpansionRef(*o.MetadataRef) {
			return fail()
		}
		node := compiled.Nodes[i*3+2]
		ref, exists := refs[node.Key]
		if !exists {
			return fail()
		}
		task, err := PrepareIngestTask(node, ref, item.Bytes, o.Metadata)
		if err != nil {
			return fail()
		}
		plan.Tasks = append(plan.Tasks, IngestDecodeTask{Task: task, Metadata: *o.MetadataRef})
	}
	if waiting {
		return IngestDecodePlan{Ready: false}, nil
	}
	return plan, nil
}
