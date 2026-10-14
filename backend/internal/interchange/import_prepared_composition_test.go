package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type compositionInnerStub struct{ calls int }

func (s *compositionInnerStub) QualifyPreparedImportRecipe(_ context.Context, _ v2input.Ref, _ []byte, _ v2input.SessionVersionBinding) error {
	s.calls++
	return nil
}

func TestImportPreparedCompositionUsesOrderedCurrentDependencies(t *testing.T) {
	for _, mode := range []string{"valid", "revoked", "changed", "probe"} {
		t.Run(mode, func(t *testing.T) {
			makeRef := func(body []byte) v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(body)} }
			sourceRaw, styleRaw, firstRaw, secondRaw := []byte(`{}`), []byte(`{"style":1}`), []byte(`{"revision":1}`), []byte(`{"revision":2}`)
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: makeRef(sourceRaw), CanvasHash: rawSHA([]byte("canvas"))}
			style, first, second := makeRef(styleRaw), makeRef(firstRaw), makeRef(secondRaw)
			raw, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{first, second}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": map[string]any{"ev": 0, "log_r": 0, "log_g": 0, "log_b": 0, "saturation_delta": 0, "retouch_delta": 0}})
			if err != nil {
				t.Fatal(err)
			}
			ref := makeRef(raw)
			bodies := map[v2input.Entry][]byte{{Ref: style, Contract: "StyleV2"}: styleRaw, {Ref: first, Contract: "Override"}: firstRaw, {Ref: second, Contract: "Override"}: secondRaw}
			calls := 0
			cause := errors.New("current authority revoked")
			resolver := preparedRecipeNodeFunc(func(_ context.Context, entry v2input.Entry) (ImportClosureNode, error) {
				calls++
				if mode == "revoked" && calls > 3 {
					return ImportClosureNode{}, cause
				}
				body, ok := bodies[entry]
				if !ok {
					return ImportClosureNode{}, v2input.ErrInvalid
				}
				if mode == "changed" && calls > 3 {
					body = []byte(`{"changed":true}`)
				}
				return ImportClosureNode{Entry: entry, Body: append([]byte(nil), body...)}, nil
			})
			probe := &exportCompositionProbeStub{runtime: rawSHA([]byte("runtime"))}
			if mode == "probe" {
				probe.err = cause
			}
			native, err := NewNativeExportStyleValidator(&ExportArchiveScratch{root: t.TempDir(), maximum: 1 << 20}, probe, map[string]string{}, probe.runtime, exportCompositionStagingValidator{})
			if err != nil {
				t.Fatal(err)
			}
			native.contract = "RenderComposition"
			inner := &compositionInnerStub{}
			scope := ImportPreparedScope{Tenant: uuid.New(), Run: uuid.New(), Workflow: uuid.New(), Entry: uuid.New(), Binding: binding}
			q := ImportPreparedCompositionQualifier{Inner: inner, Scope: scope, Resolver: resolver, Masters: &importMasterReaderStub{body: sourceRaw}, Native: native}
			err = q.QualifyPreparedImportRecipe(context.Background(), ref, raw, binding)
			if (err == nil) != (mode == "valid") || inner.calls != 1 {
				t.Fatal("composition outcome", mode, err, inner.calls)
			}
			if mode == "valid" && (calls != 6 || probe.calls != 1 || len(probe.body.Overrides) != 2 || probe.body.Overrides[0].SHA != first.SHA256 || probe.body.Overrides[1].SHA != second.SHA256) {
				t.Fatal("ordered composition not rechecked")
			}
			if (mode == "revoked" || mode == "probe") && !errors.Is(err, cause) {
				t.Fatal("authority failure lost", err)
			}
		})
	}
}
