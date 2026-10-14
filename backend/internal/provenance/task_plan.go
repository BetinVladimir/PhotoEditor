package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// These estimates must come from the installed operator qualification layer,
// not HTTP. Compilation checks bookkeeping floors, not native peak memory.
type SigningTaskResources struct {
	ContractsSHA                                           string
	RAMBytes, ScratchBytes, OutputBytes, MaxInputJSONBytes int64
}
type FrozenSigningTask struct {
	resources SigningTaskResources
	task      v2input.DraftTask
	params    v2input.Ref
	body      []byte
	inputs    []v2input.Entry
}

func (p FrozenSigningTask) Task() v2input.DraftTask {
	t := p.task
	t.ExecutionProfile = append(json.RawMessage(nil), t.ExecutionProfile...)
	return t
}
func (p FrozenSigningTask) ParamsDocument() (v2input.Ref, []byte) {
	return p.params, append([]byte(nil), p.body...)
}
func (p FrozenSigningTask) Inputs() []v2input.Entry { return append([]v2input.Entry(nil), p.inputs...) }
func (p FrozenSigningTask) Resources() SigningTaskResources { return p.resources }

// CompileSigningTask builds a blocked task specification. It neither reserves
// quota nor activates a workflow. Admission must recheck source authority,
// operator qualification and these resource estimates under its own locks.
func CompileSigningTask(ctx context.Context, intent FrozenSigningIntent, registry *SigningPolicyRegistry, mime string, resources SigningTaskResources, now time.Time) (FrozenSigningTask, error) {
	return compileSigningTask(ctx, intent, registry, mime, resources, now, false)
}

// CompileDelegatedSigningTask applies the same resource and policy checks to
// a parent-delegated intent, without inventing an editor session.
func CompileDelegatedSigningTask(ctx context.Context, intent FrozenSigningIntent,
	registry *SigningPolicyRegistry, mime string, resources SigningTaskResources,
	now time.Time) (FrozenSigningTask, error) {
	return compileSigningTask(ctx, intent, registry, mime, resources, now, true)
}

func compileSigningTask(ctx context.Context, intent FrozenSigningIntent, registry *SigningPolicyRegistry, mime string, resources SigningTaskResources, now time.Time, delegated bool) (FrozenSigningTask, error) {
	var zero FrozenSigningTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !registry.matchesTenant(intent.tenant) || intent.actor == uuid.Nil ||
		intent.delegated != delegated ||
		(delegated && intent.session != uuid.Nil) ||
		(!delegated && intent.session == uuid.Nil) ||
		len(intent.key) < 1 || len(intent.key) > 128 || intent.workflow != uuid.NewSHA1(intent.tenant, []byte("provenance:"+intent.actor.String()+":"+intent.key)) || intent.tenant == uuid.Nil || intent.project == uuid.Nil || intent.export == uuid.Nil || intent.workflow == uuid.Nil || intent.run != uuid.NewSHA1(intent.workflow, []byte("run")) || intent.task != uuid.NewSHA1(intent.workflow, []byte("provenance.sign:0")) || !digest(intent.requestSHA) || bodyDigest(intent.request) != intent.requestSHA {
		return zero, ErrInvalid
	}
	params, err := DecodeSignParams(intent.params)
	if err != nil {
		return zero, err
	}
	var request struct {
		Export uuid.UUID `json:"export_id"`
		Policy uuid.UUID `json:"signing_policy_id"`
	}
	if decode(intent.request, &request, 1024) != nil || request.Export != intent.export || request.Policy != params.PolicyID {
		return zero, ErrInvalid
	}
	if _, err := DecodeSigningLineage(intent.lineage, params.Lineage); err != nil {
		return zero, err
	}
	policy, err := registry.Resolve(ctx, params, mime, now)
	if err != nil {
		return zero, err
	}
	const safe int64 = 9007199254740991
	// The SDK destination is currently bounded to 512MiB, the registry permits
	// an 8MiB C2PA manifest and an 8MiB SignedResult document. Reserve all three.
	const outputs int64 = (512 << 20) + (8 << 20) + (8 << 20)
	if !digest(resources.ContractsSHA) || resources.MaxInputJSONBytes < int64(len(intent.lineage))+(1<<20) || resources.MaxInputJSONBytes > 128<<20 || resources.RAMBytes < resources.MaxInputJSONBytes || resources.RAMBytes > safe || resources.ScratchBytes < 1<<30 || resources.ScratchBytes > safe || resources.OutputBytes < outputs || resources.OutputBytes > safe {
		return zero, ErrInvalid
	}
	entries := []v2input.Entry{{Ref: params.Export, Contract: "QualifiedExport"}, {Ref: params.Lineage, Contract: "SigningLineage"}, {Ref: params.Policy, Contract: "SigningPolicy"}}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Ref.ArtifactID == entries[i].Ref.ArtifactID {
			return zero, ErrInvalid
		}
	}
	inputDigest, err := canonical.Hash(entries)
	if err != nil {
		return zero, err
	}
	paramsRef := v2input.Ref{ArtifactID: uuid.NewSHA1(intent.task, []byte("params")), SHA256: bodyDigest(intent.params)}
	for _, entry := range entries {
		if entry.Ref.ArtifactID == paramsRef.ArtifactID {
			return zero, ErrInvalid
		}
	}
	profile, err := canonical.Marshal(map[string]any{"backend": "cpu", "renderer_version": "v2.2", "execution_pool": "signer", "contracts_sha": resources.ContractsSHA, "max_input_json_bytes": resources.MaxInputJSONBytes, "sdk_lock_sha": policy.SDKLockSHA, "certificate_chain_sha": policy.CertificateChainSHA, "trust_bundle_sha": policy.TrustBundleSHA, "resource_estimate": map[string]int64{"ram_bytes": resources.RAMBytes, "vram_bytes": 0, "scratch_bytes": resources.ScratchBytes, "output_bytes": resources.OutputBytes, "pixel_count": 0}})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return FrozenSigningTask{resources: resources, task: v2input.DraftTask{ID: intent.task, NodeKey: "provenance.sign:0", Operation: "provenance.sign", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: paramsRef.ArtifactID, ParamsSHA: paramsRef.SHA256, InputDigest: inputDigest, ExecutionProfile: profile}, params: paramsRef, body: append([]byte(nil), intent.params...), inputs: entries}, nil
}
