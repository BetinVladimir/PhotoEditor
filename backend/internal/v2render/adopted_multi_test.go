package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"strings"
	"testing"
)

func multiReadFixture(t *testing.T, operation string) (AdoptedMultiBinding, uuid.UUID, []byte) {
	t.Helper()
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	frames := []v2input.SessionVersionBinding{{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}, {AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("c", 64)}}
	var params []byte
	var err error
	if operation == "optical.panorama" {
		params, _, err = (v2optical.PanoramaParams{SchemaVersion: 1, Frames: frames, ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: ref(), Projection: "planar"}).Compile()
	} else {
		params, _, err = (v2optical.FocusStackParams{SchemaVersion: 1, Frames: frames, ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: ref(), MotionPolicy: "review"}).Compile()
	}
	if err != nil {
		t.Fatal(err)
	}
	document := v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: ref(), ChangedMask: ref(), Report: ref()}
	raw, err := canonical.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	root := ref()
	root.SHA256 = hex.EncodeToString(sum[:])
	run := uuid.New()
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.TaskID(workflow, operation, 0)
	if err != nil {
		t.Fatal(err)
	}
	return AdoptedMultiBinding{ResultID: uuid.New(), Result: root, Run: run, Workflow: workflow, Snapshot: uuid.New(), Operation: operation, ParamsJCS: string(params)}, task, raw
}

type multiReadSource struct {
	binding AdoptedMultiBinding
	object  objectstore.Object
	calls   int
	after   func(*multiReadSource)
}

func (s *multiReadSource) LoadObject(_ context.Context, _ auth.Identity, _, _, _ uuid.UUID, _ string, _ v2input.Ref, _ string) (AdoptedMultiBinding, objectstore.Object, error) {
	s.calls++
	if s.calls > 1 && s.after != nil {
		s.after(s)
	}
	return s.binding, s.object, nil
}

type multiReadStream struct {
	io.Reader
	closed   *int
	closeErr error
}

func (s multiReadStream) Close() error { *s.closed++; return s.closeErr }

type multiReadObjects struct {
	raw            []byte
	opened, closed int
	before         func()
	closeErr       error
}

func (o *multiReadObjects) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	o.opened++
	if o.before != nil {
		o.before()
	}
	return multiReadStream{bytes.NewReader(o.raw), &o.closed, o.closeErr}, nil
}
func multiReaderSource(binding AdoptedMultiBinding, raw []byte) *multiReadSource {
	return &multiReadSource{binding: binding, object: objectstore.Object{Key: "adopted/root", VersionID: "exact-version", SHA256: binding.Result.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}}
}
func loadMultiTest(ctx context.Context, s *multiReadSource, o *multiReadObjects, task uuid.UUID, operation string, expected v2input.Ref) (AdoptedMultiBinding, v2draft.ImageResultDocument, error) {
	return LoadAdoptedMultiResult(ctx, s, o, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, operation, expected, "photos")
}

func TestAdoptedMultiRootBothOperations(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			binding, task, raw := multiReadFixture(t, operation)
			source := multiReaderSource(binding, raw)
			objects := &multiReadObjects{raw: raw}
			got, result, err := loadMultiTest(context.Background(), source, objects, task, operation, binding.Result)
			if err != nil || got != binding || result.GeneratedMask != nil || source.calls != 2 || objects.opened != 1 || objects.closed != 1 {
				t.Fatalf("read: %v %d %+v", err, source.calls, objects)
			}
			frames, reference, _, err := got.Inputs()
			if err != nil || reference != 1 || len(frames) != 2 {
				t.Fatal("ordered inputs lost")
			}
			first := frames[0]
			frames[0] = frames[1]
			again, _, _, _ := got.Inputs()
			if again[0] != first {
				t.Fatal("binding mutable through parsed inputs")
			}
		})
	}
}
func TestAdoptedMultiRootRefusesChangedAuthorityOrLocator(t *testing.T) {
	for _, field := range []string{"snapshot", "params", "version", "key", "length"} {
		t.Run(field, func(t *testing.T) {
			binding, task, raw := multiReadFixture(t, "optical.panorama")
			source := multiReaderSource(binding, raw)
			objects := &multiReadObjects{raw: raw}
			source.after = func(s *multiReadSource) {
				switch field {
				case "snapshot":
					s.binding.Snapshot = uuid.New()
				case "params":
					s.binding.ParamsJCS = strings.Replace(s.binding.ParamsJCS, "planar", "cylindrical", 1)
				case "version":
					s.object.VersionID = "new-version"
				case "key":
					s.object.Key = "different/key"
				case "length":
					s.object.Bytes++
				}
			}
			_, _, err := loadMultiTest(context.Background(), source, objects, task, binding.Operation, binding.Result)
			if !errors.Is(err, v2input.ErrExpired) || objects.closed != 1 {
				t.Fatalf("changed authority accepted: %v", err)
			}
		})
	}
}
func TestAdoptedMultiRootRefusesBeforeObjectIO(t *testing.T) {
	for _, field := range []string{"producer", "operation", "version", "media", "oversize", "params", "root"} {
		t.Run(field, func(t *testing.T) {
			binding, task, raw := multiReadFixture(t, "optical.panorama")
			source := multiReaderSource(binding, raw)
			objects := &multiReadObjects{raw: raw}
			operation := binding.Operation
			switch field {
			case "producer":
				task = uuid.New()
			case "operation":
				operation = "optical.focus_stack"
			case "version":
				source.object.VersionID = "null"
			case "media":
				source.object.ContentType = "image/jpeg"
			case "oversize":
				source.object.Bytes = 1048577
			case "params":
				source.binding.ParamsJCS = "{}"
			case "root":
				source.binding.Result.ArtifactID = uuid.New()
			}
			if _, _, err := loadMultiTest(context.Background(), source, objects, task, operation, binding.Result); err == nil || objects.opened != 0 {
				t.Fatal("unqualified root reached IO")
			}
		})
	}
}
func TestAdoptedMultiRootClosesMalformedStreams(t *testing.T) {
	for _, field := range []string{"sha", "extra", "short", "close", "cancel", "noncanonical"} {
		t.Run(field, func(t *testing.T) {
			binding, task, raw := multiReadFixture(t, "optical.panorama")
			source := multiReaderSource(binding, raw)
			objects := &multiReadObjects{raw: bytes.Clone(raw)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch field {
			case "sha":
				objects.raw[0] = '['
			case "extra":
				objects.raw = append(objects.raw, ' ')
			case "short":
				objects.raw = objects.raw[:len(raw)-1]
			case "close":
				objects.closeErr = errors.New("close failed")
			case "cancel":
				objects.before = cancel
			case "noncanonical":
				objects.raw = append([]byte(" "), raw...)
				sum := sha256.Sum256(objects.raw)
				binding.Result.SHA256 = hex.EncodeToString(sum[:])
				source.binding = binding
				source.object.SHA256 = binding.Result.SHA256
				source.object.Bytes = int64(len(objects.raw))
			}
			if _, _, err := loadMultiTest(ctx, source, objects, task, binding.Operation, binding.Result); err == nil || objects.closed != 1 || source.calls != 1 {
				t.Fatalf("invalid bytes accepted or no close: %v", err)
			}
		})
	}
}
func TestAdoptedMultiResultForbidsSourceReuseAndGeneratedMask(t *testing.T) {
	binding, _, raw := multiReadFixture(t, "optical.focus_stack")
	result, err := v2draft.ParseImageResult(raw, binding.Result)
	if err != nil {
		t.Fatal(err)
	}
	frames, _, _, _ := binding.Inputs()
	result.WorkingImage = frames[0].Source
	if validateMultiResultRoot(binding, result) == nil {
		t.Fatal("source reused as multi output")
	}
	result, err = v2draft.ParseImageResult(raw, binding.Result)
	if err != nil {
		t.Fatal(err)
	}
	result.GeneratedMask = &result.ChangedMask
	if validateMultiResultRoot(binding, result) == nil {
		t.Fatal("generated mask accepted")
	}
}
