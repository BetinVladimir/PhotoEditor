package interchange

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type exportCompositionReaderStub struct {
	nodes     map[v2input.Entry]ExportClosureNode
	calls     int
	failAfter int
	cause     error
}

func (s *exportCompositionReaderStub) ResolveExportNode(ctx context.Context, entry v2input.Entry) (ExportClosureNode, error) {
	s.calls++
	if err := ctx.Err(); err != nil {
		return ExportClosureNode{}, err
	}
	if s.failAfter > 0 && s.calls > s.failAfter {
		return ExportClosureNode{}, s.cause
	}
	node, ok := s.nodes[entry]
	if !ok {
		return ExportClosureNode{}, v2input.ErrInvalid
	}
	return node, nil
}

type exportCompositionProbeStub struct {
	runtime string
	calls   int
	body    exportCompositionBundle
	policy  map[string]map[string]string
	err     error
}

func (s *exportCompositionProbeStub) RunExportNativeProbe(ctx context.Context, raw []byte) ([]byte, error) {
	s.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var request struct {
		Schema     int    `json:"schema_version"`
		Contract   string `json:"contract"`
		Path       string `json:"file_path"`
		SHA        string `json:"sha256"`
		Bytes      int64  `json:"bytes"`
		Policy     string `json:"model_policy_sha256"`
		PolicyPath string `json:"model_policy_path"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	if request.Schema != 3 || request.Contract != "RenderComposition" {
		return nil, v2input.ErrInvalid
	}
	body, err := os.ReadFile(request.Path)
	if err != nil {
		return nil, err
	}
	if rawSHA(body) != request.SHA || int64(len(body)) != request.Bytes {
		return nil, v2input.ErrInvalid
	}
	if err := json.Unmarshal(body, &s.body); err != nil {
		return nil, err
	}
	policy, err := os.ReadFile(request.PolicyPath)
	if err != nil {
		return nil, err
	}
	if rawSHA(policy) != request.Policy {
		return nil, v2input.ErrInvalid
	}
	if err := json.Unmarshal(policy, &s.policy); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	return canonical.Marshal(ExportNativeStyleReceipt{Schema: 1, Contract: request.Contract, SHA: request.SHA, Bytes: request.Bytes, Policy: request.Policy, Runtime: s.runtime})
}

// Mock native receipts exercise exact bundle construction and IO authority;
// production Python schema/merge behavior has separate worker tests.
func TestExportCompositionBuildsOrderedBundleAndRechecksDocuments(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "hash", "identity", "recheck", "probe", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ref := func(raw []byte) v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)} }
			sourceRaw := []byte(`{}`)
			source := ref(sourceRaw)
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: rawSHA([]byte("canvas"))}
			styleRaw := []byte(`{"style":1}`)
			style := ref(styleRaw)
			first, second := []byte(`{"revision":1}`), []byte(`{"revision":2}`)
			one, two := ref(first), ref(second)
			authored, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{one, two}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}})
			if err != nil {
				t.Fatal(err)
			}
			icc, model := rawSHA([]byte("icc")), rawSHA([]byte("model"))
			raw, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, Overrides: []v2draft.ResolvedCorrection{{Ref: one}, {Ref: two}}, ICCLockSHA: icc, ModelLockSHA: model}, nil)
			if err != nil {
				t.Fatal(err)
			}
			parent := v2input.Entry{Ref: ref(raw), Contract: "FrozenRenderRecipe"}
			base := ExportFrozenDocumentValidator{recipes: map[uuid.UUID]exportRecipeBinding{parent.Ref.ArtifactID: {ref: parent.Ref, binding: binding}}, icc: icc, model: model}
			reader := &exportCompositionReaderStub{nodes: map[v2input.Entry]ExportClosureNode{}}
			entries := []v2input.Entry{{Ref: source, Contract: "WorkingImage"}, {Ref: style, Contract: "StyleV2"}, {Ref: one, Contract: "Override"}, {Ref: two, Contract: "Override"}}
			for index, body := range [][]byte{sourceRaw, styleRaw, first, second} {
				reader.nodes[entries[index]] = ExportClosureNode{Entry: entries[index], Body: body}
			}
			root := t.TempDir()
			scratch := &ExportArchiveScratch{root: root, maximum: 1 << 20}
			probe := &exportCompositionProbeStub{runtime: rawSHA([]byte("runtime"))}
			validator, err := newExportFrozenCompositionValidatorWithPolicies(base, reader, scratch, probe, map[string]string{"mobilesam": "MobileSAM/mobile_sam.pt.torchscript"}, map[string]string{"mobilesam": rawSHA([]byte("model-lock"))}, probe.runtime)
			if err != nil {
				t.Fatal(err)
			}
			cause := errors.New("authority revoked")
			ctx := context.Background()
			switch mode {
			case "missing":
				delete(reader.nodes, entries[1])
			case "hash":
				node := reader.nodes[entries[1]]
				node.Body = []byte(`{"changed":true}`)
				reader.nodes[entries[1]] = node
			case "identity":
				node := reader.nodes[entries[1]]
				node.Entry.Ref.ArtifactID = uuid.New()
				reader.nodes[entries[1]] = node
			case "recheck":
				reader.failAfter = 4
				reader.cause = cause
			case "probe":
				probe.err = cause
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			children, err := validator.ExportDocumentChildren(ctx, parent, raw)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("unexpected composition: %v", err)
			}
			if mode == "valid" {
				if len(children) != 4 || reader.calls != 8 || probe.calls != 1 || len(probe.body.Overrides) != 2 || probe.body.Overrides[0].SHA != one.SHA256 || probe.body.Overrides[1].SHA != two.SHA256 {
					t.Fatal("bundle order/authority incomplete")
				}
				if probe.policy["style_weights"]["mobilesam"] != "MobileSAM/mobile_sam.pt.torchscript" || probe.policy["operation_locks"]["mobilesam"] != rawSHA([]byte("model-lock")) {
					t.Fatal("style weights and operation locks conflated")
				}
			}
			if (mode == "recheck" || mode == "probe") && !errors.Is(err, cause) {
				t.Fatal("failure cause lost")
			}
			files, err := os.ReadDir(root)
			if err != nil || len(files) != 0 || scratch.used != 0 {
				t.Fatal("composition scratch leak", err)
			}
		})
	}
}
