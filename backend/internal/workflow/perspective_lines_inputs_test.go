package workflow

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/google/uuid"
	"io"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2optical"
	"testing"
)

type linesInputsFixture struct {
	plan                        PerspectiveLinesPlan
	source                      objectstore.Object
	arrays                      map[uuid.UUID]objectstore.Object
	bodies                      map[string][]byte
	sourceCalls                 int
	arrayCalls                  map[uuid.UUID]int
	sourceChanged, arrayChanged bool
	opens, closes               int
}
type linesInputStream struct {
	io.Reader
	onClose func()
}

func (s linesInputStream) Close() error { s.onClose(); return nil }
func (f *linesInputsFixture) ResolveSource(context.Context, PerspectiveLinesScope, uuid.UUID, auth.Identity) (objectstore.Object, error) {
	f.sourceCalls++
	o := f.source
	if f.sourceChanged && f.sourceCalls > 1 {
		o.VersionID = "changed"
	}
	return o, nil
}
func (f *linesInputsFixture) ResolvePerspectiveSourceArray(_ context.Context, _ auth.Identity, _, _, _ uuid.UUID, _ v2optical.PerspectiveParams, _ []byte, spec v2optical.PerspectiveSourceArraySpec) (objectstore.Object, error) {
	f.arrayCalls[spec.Ref.ArtifactID]++
	o := f.arrays[spec.Ref.ArtifactID]
	if f.arrayChanged && f.arrayCalls[spec.Ref.ArtifactID] > 1 {
		o.VersionID = "changed"
	}
	return o, nil
}
func (f *linesInputsFixture) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	f.opens++
	return linesInputStream{Reader: bytes.NewReader(f.bodies[o.Key]), onClose: func() { f.closes++ }}, nil
}
func linesNPY(channels int, value float32) []byte {
	shape := "(3, 4)"
	count := 12
	if channels > 0 {
		shape = fmt.Sprintf("(3, 4, %d)", channels)
		count *= channels
	}
	header := []byte("{'descr': '<f4', 'fortran_order': False, 'shape': " + shape + ", }")
	for (10+len(header)+1)%64 != 0 {
		header = append(header, ' ')
	}
	header = append(header, '\n')
	raw := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(raw[8:10], uint16(len(header)))
	raw = append(raw, header...)
	for i := 0; i < count; i++ {
		var encoded [4]byte
		binary.LittleEndian.PutUint32(encoded[:], math.Float32bits(value))
		raw = append(raw, encoded[:]...)
	}
	return raw
}
func newLinesInputsFixture(t *testing.T, alpha bool) *linesInputsFixture {
	t.Helper()
	scope, raw, icc := perspectiveLinesFixture(t, alpha)
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	f := &linesInputsFixture{arrays: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}, arrayCalls: map[uuid.UUID]int{}}
	put := func(ref *review.ArtifactRef, key string, raw []byte) {
		ref.SHA256 = renderDerivativeBytesSHA(raw)
		f.bodies[key] = raw
		f.arrays[ref.ID] = objectstore.Object{Key: key, VersionID: "version1", SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/x-npy"}
	}
	put(&image.Pixels, "rgb", linesNPY(3, 2.))
	if image.Alpha != nil {
		put(image.Alpha, "alpha", linesNPY(0, .5))
	}
	raw, err = canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	scope.Base.Source.SHA256 = renderDerivativeBytesSHA(raw)
	f.plan, err = CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	f.source = objectstore.Object{Key: "source", VersionID: "version1", SHA256: scope.Base.Source.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}
	f.bodies["source"] = raw
	return f
}
func (f *linesInputsFixture) verify(ctx context.Context) (VerifiedPerspectiveLinesInputs, error) {
	return VerifyPerspectiveLinesInputs(ctx, f, f, f, f.plan, uuid.New(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()})
}
func TestPerspectiveLinesInputsVerifyFullHDRAndAlphaAndCloseStreams(t *testing.T) {
	for _, alpha := range []bool{false, true} {
		f := newLinesInputsFixture(t, alpha)
		verified, err := f.verify(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		count := 1
		if alpha {
			count = 2
		}
		if len(verified.Arrays()) != count || verified.Source() != f.source || f.opens != count+1 || f.closes != f.opens {
			t.Fatal("incomplete verification")
		}
		exposed := verified.Arrays()
		exposed[0].Object.VersionID = "forged"
		if verified.Arrays()[0].Object.VersionID == "forged" {
			t.Fatal("mutable witnesses exposed")
		}
	}
}
func TestPerspectiveLinesInputsRejectChangedObjectVersionsAndBytes(t *testing.T) {
	for _, fault := range []string{"source_version", "array_version", "source_bytes", "rgb_bytes", "alpha_bytes"} {
		f := newLinesInputsFixture(t, true)
		switch fault {
		case "source_version":
			f.sourceChanged = true
		case "array_version":
			f.arrayChanged = true
		case "source_bytes":
			f.bodies["source"] = append(f.bodies["source"], ' ')
		case "rgb_bytes":
			f.bodies["rgb"][len(f.bodies["rgb"])-1] ^= 1
		case "alpha_bytes":
			f.bodies["alpha"] = f.bodies["alpha"][:len(f.bodies["alpha"])-1]
		}
		if _, err := f.verify(context.Background()); err == nil {
			t.Fatal("invalid input accepted", fault)
		}
		if f.closes != f.opens {
			t.Fatal("stream leaked", fault)
		}
	}
}
func TestPerspectiveLinesInputsRejectInvalidLocatorBeforeOpenAndCancellation(t *testing.T) {
	f := newLinesInputsFixture(t, false)
	for id, o := range f.arrays {
		o.VersionID = "null"
		f.arrays[id] = o
	}
	if _, err := f.verify(context.Background()); err == nil {
		t.Fatal("unversioned input accepted")
	}
	if f.opens != 1 {
		t.Fatal("opened invalid array")
	}
	f = newLinesInputsFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.verify(ctx); err != context.Canceled || f.opens != 0 {
		t.Fatal(err)
	}
	if _, err := (SQLPerspectiveLinesSources{}).ResolveSource(ctx, PerspectiveLinesScope{}, uuid.Nil, auth.Identity{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := (SQLPerspectiveLinesSources{}).ResolveSource(context.Background(), PerspectiveLinesScope{}, uuid.Nil, auth.Identity{}); err == nil {
		t.Fatal("empty source scope accepted")
	}
}
