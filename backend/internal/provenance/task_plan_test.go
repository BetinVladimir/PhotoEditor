package provenance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

func TestSigningTaskCompilationPinsDedicatedPoolAndOwnsDocuments(t *testing.T) {
	config, params, now := signingRegistryFixture(t)
	registry, err := NewSigningPolicyRegistry(config, &signingAuthorityStub{})
	if err != nil {
		t.Fatal(err)
	}
	lineage, ref := contractBytes(t, SigningLineage{Schema: 1, SourceHashes: []string{params.Export.SHA256}, Actions: []SigningAction{}})
	export := uuid.New()
	source := &signingSourceStub{evidence: SigningExportEvidence{Project: uuid.New(), ExportID: export, Export: params.Export, MIME: "image/jpeg", Lineage: ref, LineageBody: lineage}}
	intent, err := PrepareSigningIntent(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), export, params.PolicyID, "compile", source, registry, now)
	if err != nil {
		t.Fatal(err)
	}
	resources := SigningTaskResources{ContractsSHA: strings.Repeat("d", 64), RAMBytes: 32 << 20, ScratchBytes: 1 << 30, OutputBytes: (512 << 20) + (16 << 20), MaxInputJSONBytes: 16 << 20}
	compiled, err := CompileSigningTask(context.Background(), intent, registry, "image/jpeg", resources, now)
	if err != nil {
		t.Fatal(err)
	}
	task := compiled.Task()
	if task.ID != intent.TaskID() || task.Operation != "provenance.sign" || task.OperationVersion != "1" || task.ResourceClass != "cpu" {
		t.Fatal("wrong task")
	}
	var profile map[string]any
	if json.Unmarshal(task.ExecutionProfile, &profile) != nil || profile["execution_pool"] != "signer" || profile["backend"] != "cpu" {
		t.Fatal("signer pool missing")
	}
	inputs := compiled.Inputs()
	hash, err := canonical.Hash(inputs)
	if err != nil || hash != task.InputDigest || len(inputs) != 3 {
		t.Fatal("input closure changed")
	}
	seen := map[string]bool{}
	for _, entry := range inputs {
		seen[entry.Contract] = true
	}
	if !seen["QualifiedExport"] || !seen["SigningLineage"] || !seen["SigningPolicy"] {
		t.Fatal("missing typed input")
	}
	doc, body := compiled.ParamsDocument()
	decoded, err := DecodeSignParams(body)
	if err != nil || decoded.Policy != params.Policy || decoded.Export != params.Export || decoded.Lineage != ref || doc.SHA256 != bodyDigest(body) || task.ParamsArtifactID != doc.ArtifactID {
		t.Fatal("params binding lost")
	}
	task.ExecutionProfile[0] = 'x'
	body[0] = 'x'
	inputs[0].Contract = "wrong"
	_, again := compiled.ParamsDocument()
	if again[0] != '{' || compiled.Task().ExecutionProfile[0] != '{' || compiled.Inputs()[0].Contract == "wrong" {
		t.Fatal("mutable compiled plan")
	}
	for _, mode := range []string{"zero_intent", "request_hash", "request_body", "params", "lineage", "run", "task", "contracts", "ram", "scratch", "output", "json", "expired", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			i, r, clock, ctx := intent, resources, now, context.Background()
			switch mode {
			case "zero_intent":
				i = FrozenSigningIntent{}
			case "request_hash":
				i.requestSHA = strings.Repeat("f", 64)
			case "request_body":
				i.request = []byte(`{}`)
				i.requestSHA = bodyDigest(i.request)
			case "params":
				i.params = []byte(`{}`)
			case "lineage":
				i.lineage = []byte(`{}`)
			case "run":
				i.run = uuid.New()
			case "task":
				i.task = uuid.New()
			case "contracts":
				r.ContractsSHA = "invalid"
			case "ram":
				r.RAMBytes = 1
			case "scratch":
				r.ScratchBytes = 1
			case "output":
				r.OutputBytes = 1
			case "json":
				r.MaxInputJSONBytes = 1
			case "expired":
				clock = now.AddDate(20, 0, 0)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if value, err := CompileSigningTask(ctx, i, registry, "image/jpeg", r, clock); err == nil || value.Task().ID != uuid.Nil {
				t.Fatal("invalid plan compiled")
			}
		})
	}
}
