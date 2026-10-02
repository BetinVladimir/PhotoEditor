package v2input

import (
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestCaptureClosureRejectsSpoofedParamsAndRefTypeCollision(t *testing.T) {
	ref := func(n int) Ref {
		return Ref{uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", n)), fmt.Sprintf("%064x", n)}
	}
	params := CaptureAssessParams{SchemaVersion: 1, Binding: SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(1), CanvasHash: fmt.Sprintf("%064x", 10)},
		Proxy: ref(2), Faces: ref(3), Context: ref(4), ModelLockSHA: fmt.Sprintf("%064x", 11),
		Policy: CapturePolicy{SchemaVersion: 1, Checks: []string{"focus", "duplicate"}, MaxQueueAgeSeconds: 30, Calibration: ref(5)}}
	hash, err := canonical.Hash(params)
	if err != nil {
		t.Fatal(err)
	}
	published := Ref{uuid.New(), hash}
	source, proxy := CaptureImageInputs{Pixels: ref(6)}, CaptureImageInputs{Pixels: ref(7)}
	if _, err = CompileCaptureClosure(uuid.New(), params, published, source, proxy, ref(8), ref(9)); err != nil {
		t.Fatal(err)
	}
	bad := published
	bad.SHA256 = fmt.Sprintf("%064x", 100)
	if _, err = CompileCaptureClosure(uuid.New(), params, bad, source, proxy, ref(8), ref(9)); err == nil {
		t.Fatal("accepted wrong params SHA")
	}
	proxy.Pixels = params.Faces
	if _, err = CompileCaptureClosure(uuid.New(), params, published, source, proxy, ref(8), ref(9)); err == nil {
		t.Fatal("accepted artifact type collision")
	}
	params.Policy.Checks = []string{"focus", "focus"}
	if _, err = CompileCaptureClosure(uuid.New(), params, published, source, proxy, ref(8), ref(9)); err == nil {
		t.Fatal("accepted duplicate checks")
	}
}
