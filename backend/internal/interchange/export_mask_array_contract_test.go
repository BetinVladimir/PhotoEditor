package interchange

import (
	"testing"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestNativeMaskArrayGeometryRemainsClosedAndBounded(t *testing.T) {
	entry := v2input.Entry{Contract:"MaskArray",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("mask"))}}
	geometry := ExportArrayGeometry{Width:400,Height:300,CanvasSHA:rawSHA([]byte("canvas")),DType:"float32"}
	maximum, err := exportArrayMaximumBytes(entry,geometry)
	if err != nil || maximum != 400*300*4+exportNPYOverhead { t.Fatal("native scalar mask geometry refused",maximum,err) }
	for _, contract := range []string{"MaskPixels","UncheckedBinary","application/x-npy"} {
		changed:=entry;changed.Contract=contract
		if _,err:=exportArrayMaximumBytes(changed,geometry);err==nil { t.Fatal("untranslated or unknown contract admitted",contract) }
	}
	geometry.Width=200000001
	if _,err:=exportArrayMaximumBytes(entry,geometry);err==nil { t.Fatal("unbounded mask admitted") }
}
