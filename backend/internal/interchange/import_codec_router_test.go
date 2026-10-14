package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type importRouterCodec struct {
	kind   string
	source v2input.Ref
	calls  *int
}

func (c importRouterCodec) BindImportDocumentSource(source v2input.Ref) ImportDependencyCodec {
	c.source = source
	return c
}
func (c importRouterCodec) RemapImportDocument(_ context.Context, kind string, raw []byte, _, _ v2input.SessionVersionBinding, _ func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	(*c.calls)++
	if kind != c.kind {
		return nil, v2input.ErrInvalid
	}
	raw[0] = '!'
	return []byte(c.source.SHA256), nil
}
func (c importRouterCodec) QualifyRemappedImportDocument(_ context.Context, kind string, raw []byte, _ v2input.SessionVersionBinding) error {
	(*c.calls)++
	if kind != c.kind {
		return v2input.ErrInvalid
	}
	raw[0] = '!'
	return nil
}
func TestImportCodecRouterTypedDispatchAndSourceIsolation(t *testing.T) {
	kinds := []string{"StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef", "Params"}
	calls := 0
	codecs := []ImportDependencyCodec{}
	for _, kind := range kinds {
		codecs = append(codecs, importRouterCodec{kind: kind, calls: &calls})
	}
	router, err := NewImportRecipeCodecRouter(codecs[0], codecs[1], codecs[2], codecs[3], codecs[4], codecs[5])
	if err != nil {
		t.Fatal(err)
	}
	a := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("a"))}
	b := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("b"))}
	first, second := router.BindImportDocumentSource(a), router.BindImportDocumentSource(b)
	resolve := func(v2input.Ref, string) (v2input.Ref, error) {
		t.Fatal("unexpected child traversal")
		return v2input.Ref{}, nil
	}
	for _, kind := range kinds {
		raw := []byte(`{}`)
		result, err := first.RemapImportDocument(context.Background(), kind, raw, v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, resolve)
		if err != nil || string(result) != a.SHA256 || raw[0] != '{' {
			t.Fatal("typed/source ownership lost", kind, err)
		}
		result, err = second.RemapImportDocument(context.Background(), kind, raw, v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, resolve)
		if err != nil || string(result) != b.SHA256 {
			t.Fatal("source binding leaked", err)
		}
		if err := first.QualifyRemappedImportDocument(context.Background(), kind, raw, v2input.SessionVersionBinding{}); err != nil || raw[0] != '{' {
			t.Fatal("qualification ownership lost", err)
		}
	}
	before := calls
	if _, err := first.RemapImportDocument(context.Background(), "Unknown", []byte(`{}`), v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, resolve); err == nil {
		t.Fatal("unknown schema reached fallback")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := first.QualifyRemappedImportDocument(ctx, "StyleV2", []byte(`{}`), v2input.SessionVersionBinding{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
	if calls != before {
		t.Fatal("denied dispatch reached codec")
	}
	if _, err := NewImportRecipeCodecRouter(codecs[0], codecs[1], codecs[2], codecs[3], nil, codecs[5]); err == nil {
		t.Fatal("missing mask authority accepted")
	}
	resolved, err := router.WithResolved(importRouterCodec{kind: "ResolvedStyle", calls: &calls})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.RemapImportDocument(context.Background(), "ResolvedStyle", []byte(`{}`), v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, resolve); err == nil {
		t.Fatal("base router unexpectedly enabled resolved")
	}
	if _, err := resolved.BindImportDocumentSource(a).RemapImportDocument(context.Background(), "ResolvedStyle", []byte(`{}`), v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, resolve); err != nil {
		t.Fatal("resolved route not installed", err)
	}
}
