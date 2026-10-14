package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type binaryRouteStub struct {
	kind  string
	calls int
}

func (s *binaryRouteStub) RemapImportBinary(_ context.Context, _ uuid.UUID, archived, _ v2input.Ref, kind string, _, _ v2input.SessionVersionBinding, _ int64) (v2input.Ref, int64, error) {
	s.calls++
	if kind != s.kind {
		return v2input.Ref{}, 0, v2input.ErrInvalid
	}
	return archived, 1, nil
}
func TestImportBinaryCodecRouterKeepsMaskAndLUTRolesSeparate(t *testing.T) {
	mask, lut, labels := &binaryRouteStub{kind: "MaskPixels"}, &binaryRouteStub{kind: "AbsoluteLUTArray"}, &binaryRouteStub{kind: "DefectLabelMap"}
	router := ImportBinaryCodecRouter{Masks: mask, LUT: lut, DefectLabels: labels}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("binary"))}
	for _, kind := range []string{"MaskPixels", "AbsoluteLUTArray", "DefectLabelMap"} {
		if _, _, err := router.RemapImportBinary(context.Background(), uuid.New(), ref, ref, kind, v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if mask.calls != 1 || lut.calls != 1 || labels.calls != 1 {
		t.Fatal("binary roles crossed")
	}
	if _, _, err := router.RemapImportBinary(context.Background(), uuid.New(), ref, ref, "WorkingPixels", v2input.SessionVersionBinding{}, v2input.SessionVersionBinding{}, 1); err == nil {
		t.Fatal("uninstalled binary role accepted")
	}
}
