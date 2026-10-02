package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type maskVerifyFixture struct {
	objects       map[uuid.UUID]objectstore.Object
	bodies        map[string][]byte
	lookups       []v2draft.LayerRef
	opens, closes int
	deny          string
}

func (f *maskVerifyFixture) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	f.lookups = append(f.lookups, node)
	if node.Contract == f.deny {
		return objectstore.Object{}, v2input.ErrExpired
	}
	object, ok := f.objects[node.Ref.ArtifactID]
	if !ok || object.SHA256 != node.Ref.SHA256 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return object, nil
}

type maskVerifyReader struct {
	io.Reader
	fixture *maskVerifyFixture
}

func (r maskVerifyReader) Close() error { r.fixture.closes++; return nil }
func (f *maskVerifyFixture) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	f.opens++
	return maskVerifyReader{bytes.NewReader(f.bodies[object.Key]), f}, nil
}

func maskVerificationFixture(t *testing.T, badProbability bool) (*maskVerifyFixture, v2input.SessionVersionBinding, v2input.Ref) {
	t.Helper()
	raw, source, base := userMaskDocuments(t)
	values := make([]float32, 400*300)
	if badProbability {
		values[100] = float32(math.NaN())
	}
	array := maskNPY("{'descr':'<f4','fortran_order':False,'shape':(300,400)}\n", values)
	pixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: maskSHA(array)}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["artifact"], _ = canonical.Marshal(pixels)
	raw, err := canonical.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	mask := v2input.Ref{ArtifactID: uuid.New(), SHA256: maskSHA(raw)}
	fixture := &maskVerifyFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}
	put := func(ref v2input.Ref, body []byte, media string) {
		key := ref.ArtifactID.String()
		fixture.objects[ref.ArtifactID] = objectstore.Object{Key: key, VersionID: "pinned-1", Bytes: int64(len(body)), SHA256: ref.SHA256, ContentType: media}
		fixture.bodies[key] = body
	}
	put(base.Source, source, "application/json")
	put(mask, raw, "application/json")
	put(pixels, array, "application/x-npy")
	return fixture, base, mask
}

func TestScopedCompositionMaskChecksPinnedProbabilityArray(t *testing.T) {
	fixture, base, ref := maskVerificationFixture(t, false)
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	parsed, err := VerifyCompositionMask(context.Background(), who, tenant, base, ref, fixture, fixture)
	if err != nil || fixture.opens != 3 || fixture.closes != 3 || len(fixture.lookups) != 3 {
		t.Fatal("mask preflight", err, fixture.opens, fixture.closes)
	}
	node := fixture.lookups[2]
	if node.Contract != "MaskPixels" || node.ParentRef != ref || node.Ref.ArtifactID != parsed.Artifact.ID {
		t.Fatal("binary parent binding", node)
	}
}

func TestScopedCompositionMaskRejectsDeniedAndInvalidBytes(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	for _, contract := range []string{"WorkingImage", "MaskPixels"} {
		fixture, base, ref := maskVerificationFixture(t, false)
		fixture.deny = contract
		_, err := VerifyCompositionMask(context.Background(), who, tenant, base, ref, fixture, fixture)
		expected := 0
		if contract == "MaskPixels" {
			expected = 2
		}
		if !errors.Is(err, v2input.ErrExpired) || fixture.opens != expected || fixture.closes != expected {
			t.Fatal("denied scope opened or leaked reader", err)
		}
	}
	fixture, base, ref := maskVerificationFixture(t, true)
	if _, err := VerifyCompositionMask(context.Background(), who, tenant, base, ref, fixture, fixture); !errors.Is(err, v2input.ErrInvalid) || fixture.opens != fixture.closes {
		t.Fatal("invalid probability accepted/reader leaked", err)
	}
	fixture, base, ref = maskVerificationFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyCompositionMask(ctx, who, tenant, base, ref, fixture, fixture); !errors.Is(err, context.Canceled) || len(fixture.lookups) != 0 {
		t.Fatal("cancelled request performed IO", err)
	}
}

func TestCompositionMaskSetVerifiesSharedProtectedArrayOnce(t *testing.T) {
	for _, bad := range []bool{false, true} {
		fixture, base, mask := maskVerificationFixture(t, bad)
		refExpr, err := canonical.Marshal(map[string]any{"op": "ref", "mask": mask, "canvas_hash": base.CanvasHash})
		if err != nil {
			t.Fatal(err)
		}
		expr, err := canonical.Marshal(map[string]any{"op": "union", "left": json.RawMessage(refExpr), "right": map[string]any{"op": "invert", "operand": json.RawMessage(refExpr)}})
		if err != nil {
			t.Fatal(err)
		}
		proposal := v2input.Ref{ArtifactID: uuid.New(), SHA256: base.Source.SHA256}
		layer := Layer{ID: uuid.New(), Enabled: true, Opacity: .5, Blend: "normal", Operation: "fixture", OperationVersion: "1", Params: proposal, Mask: expr}
		compiled, err := CompileComposition(layer, base, nil, proposal, &mask, 400, 300)
		if err != nil {
			t.Fatal(err)
		}
		params := v2input.Ref{ArtifactID: uuid.New(), SHA256: maskSHA(compiled.JSON)}
		err = VerifyCompositionMasks(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), compiled, params, fixture, fixture)
		if bad && !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("invalid shared protected mask accepted", err)
		}
		if !bad && err != nil {
			t.Fatal(err)
		}
		if fixture.opens != 3 || fixture.closes != 3 {
			t.Fatal("shared mask array not deduplicated or readers leaked", fixture.opens, fixture.closes)
		}
	}
}
