package interchange

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/v2input"
)

func TestRejectUnsupportedImportDependencyNeverPublishesUnknownParams(t *testing.T) {
	codec := RejectUnsupportedImportDependency{}
	for _, kind := range []string{"Params", "UnknownContract"} {
		calls := 0
		prepared, err := codec.RemapImportDocument(context.Background(), kind, []byte(`{"image":"archive"}`),
			v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{},
			func(v2input.Ref, string) (v2input.Ref, error) { calls++; return v2input.Ref{}, nil })
		if prepared != nil || calls != 0 {
			t.Fatal("unknown dependency reached publication or resolver", kind)
		}
		unsupported, ok := err.(*ImportRecipeUnsupported)
		if !ok || unsupported.Reason == "" {
			t.Fatal("unknown dependency did not remain visibly unsupported", kind, err)
		}
		if err := codec.QualifyRemappedImportDocument(context.Background(), kind, []byte(`{}`), v2input.SessionVersionBinding{}); err == nil {
			t.Fatal("unknown dependency was qualified", kind)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := codec.RemapImportDocument(ctx, "Params", nil, v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}
