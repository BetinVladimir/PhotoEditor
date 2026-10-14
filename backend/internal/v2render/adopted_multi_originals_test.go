package v2render

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"sort"
	"strconv"
	"testing"
)

type multiOriginalInventoryTestSource struct {
	inventory MultiOriginalInventory
	calls     int
	after     func(*multiOriginalInventoryTestSource)
}

func (s *multiOriginalInventoryTestSource) LoadOriginalInventory(_ context.Context, _ auth.Identity, _, _, _ uuid.UUID, _ AdoptedMultiBinding, _ string) (MultiOriginalInventory, error) {
	s.calls++
	if s.calls > 1 && s.after != nil {
		s.after(s)
	}
	return s.inventory, nil
}
func originalInventoryFixture(t *testing.T, operation string) (AdoptedMultiBinding, uuid.UUID, MultiOriginalInventory, []byte) {
	t.Helper()
	raw, err := os.ReadFile("../v2optical/testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(image)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding, task, root := multiReadFixture(t, operation)
	frames, _, calibration, err := binding.Inputs()
	if err != nil {
		t.Fatal(err)
	}
	for i := range frames {
		frames[i].Source.SHA256 = sha
		frames[i].CanvasHash = canvas
	}
	var params []byte
	if operation == "optical.panorama" {
		params, _, err = (v2optical.PanoramaParams{SchemaVersion: 1, Frames: frames, ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: calibration, Projection: "planar"}).Compile()
	} else {
		params, _, err = (v2optical.FocusStackParams{SchemaVersion: 1, Frames: frames, ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: calibration, MotionPolicy: "review"}).Compile()
	}
	if err != nil {
		t.Fatal(err)
	}
	binding.ParamsJCS = string(params)
	inv := MultiOriginalInventory{binding: binding, bucket: "photos", root: multiReaderSource(binding, root).object}
	for i, frame := range frames {
		inv.items = append(inv.items, MultiOriginalObject{Ordinal: i, Role: "WorkingImage", Ref: frame.Source, Object: objectstore.Object{Key: "source/" + strconv.Itoa(i), VersionID: "source-version", SHA256: sha, Bytes: int64(len(raw)), ContentType: "application/json"}})
		inv.items = append(inv.items, MultiOriginalObject{Ordinal: i, Role: "WorkingPixels", Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, Object: objectstore.Object{Key: "original/pixels", VersionID: "pixels-version", SHA256: image.Pixels.SHA256, Bytes: 120128, ContentType: "application/octet-stream"}})
		inv.items = append(inv.items, MultiOriginalObject{Ordinal: i, Role: "WorkingAlpha", Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, Object: objectstore.Object{Key: "original/alpha", VersionID: "alpha-version", SHA256: image.Alpha.SHA256, Bytes: 40128, ContentType: "application/octet-stream"}})
	}
	sort.Slice(inv.items, func(i, j int) bool {
		a, b := inv.items[i], inv.items[j]
		return a.Ordinal < b.Ordinal || a.Ordinal == b.Ordinal && a.Role < b.Role
	})
	return binding, task, inv, raw
}
func originalInventoryWire(inv MultiOriginalInventory) multiOriginalInventoryWire {
	wire := multiOriginalInventoryWire{SchemaVersion: 1, Binding: inv.binding, Root: multiOriginalLocatorWire{Bucket: inv.bucket, Key: inv.root.Key, Version: inv.root.VersionID, Bytes: inv.root.Bytes, Media: inv.root.ContentType}}
	for _, item := range inv.items {
		wire.Items = append(wire.Items, multiOriginalItemWire{Ordinal: item.Ordinal, Role: item.Role, Ref: item.Ref, Bucket: inv.bucket, Key: item.Object.Key, Version: item.Object.VersionID, Bytes: item.Object.Bytes, Media: item.Object.ContentType})
	}
	return wire
}
func loadOriginalTest(ctx context.Context, source *multiOriginalInventoryTestSource, objects *multiReadObjects, binding AdoptedMultiBinding, task uuid.UUID) (VerifiedMultiOriginals, error) {
	return LoadAdoptedMultiOriginals(ctx, source, objects, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, binding, "photos")
}
func TestMultiOriginalInventoryStrictWireAndOwnership(t *testing.T) {
	binding, task, inv, _ := originalInventoryFixture(t, "optical.panorama")
	wire := originalInventoryWire(inv)
	raw, err := canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMultiOriginalInventory(raw, binding, task, "photos")
	if err != nil || !decoded.equal(inv) {
		t.Fatalf("decode %v", err)
	}
	_, _, items := decoded.Snapshot()
	items[0].Object.VersionID = "changed"
	if !decoded.equal(inv) {
		t.Fatal("inventory aliases caller")
	}
	for _, field := range []string{"order", "missing_pixels", "bucket", "role", "version", "ref", "unknown", "cross_role"} {
		t.Run(field, func(t *testing.T) {
			changed := originalInventoryWire(inv)
			switch field {
			case "order":
				changed.Items[0], changed.Items[1] = changed.Items[1], changed.Items[0]
			case "missing_pixels":
				changed.Items = changed.Items[:len(changed.Items)-1]
			case "bucket":
				changed.Items[0].Bucket = "foreign"
			case "role":
				changed.Items[0].Role = "Params"
			case "version":
				changed.Items[0].Version = "null"
			case "ref":
				changed.Items[0].Ref = inv.binding.Result
			case "cross_role":
				role := changed.Items[0].Role
				changed.Items[0] = changed.Items[2]
				changed.Items[0].Role = role
			}
			data, err := canonical.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if field == "unknown" {
				data = append([]byte(`{"unknown":1,`), data[1:]...)
			}
			if _, err := decodeMultiOriginalInventory(data, binding, task, "photos"); err == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
}

func TestMultiOriginalInventoryOwnsCallbackResultBeforeIO(t *testing.T) {
	binding, task, inv, raw := originalInventoryFixture(t, "optical.panorama")
	source := &multiOriginalInventoryTestSource{inventory: inv}
	objects := &multiReadObjects{raw: raw}
	objects.before = func() { source.inventory.items[0].Object.VersionID = "changed-during-IO" }
	if _, err := loadOriginalTest(context.Background(), source, objects, binding, task); !errors.Is(err, v2input.ErrExpired) {
		t.Fatalf("callback mutated saved inventory: %v", err)
	}
}
func TestMultiOriginalPreflightBeforeAuthorityOrObjectIO(t *testing.T) {
	binding, task, inv, raw := originalInventoryFixture(t, "optical.panorama")
	source := &multiOriginalInventoryTestSource{inventory: inv}
	objects := &multiReadObjects{raw: raw}
	bad := binding
	bad.Snapshot = uuid.Nil
	if _, err := loadOriginalTest(context.Background(), source, objects, bad, task); err == nil || source.calls != 0 || objects.opened != 0 {
		t.Fatal("bad binding reached IO")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadOriginalTest(ctx, source, objects, binding, task); !errors.Is(err, context.Canceled) || source.calls != 0 || objects.opened != 0 {
		t.Fatal("cancelled binding reached IO")
	}
}
func TestMultiOriginalDocumentsBothOperationsAndNoNPYReads(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			binding, task, inv, raw := originalInventoryFixture(t, operation)
			source := &multiOriginalInventoryTestSource{inventory: inv}
			objects := &multiReadObjects{raw: raw}
			verified, err := loadOriginalTest(context.Background(), source, objects, binding, task)
			if err != nil || source.calls != 2 || objects.opened != 2 || objects.closed != 2 {
				t.Fatalf("load %v %+v", err, objects)
			}
			documents := verified.Documents()
			documents[0][0] = '['
			if !bytes.Equal(verified.Documents()[0], raw) {
				t.Fatal("documents mutable")
			}
			images, err := verified.Images()
			if err != nil || len(images) != 2 {
				t.Fatal(err)
			}
			images[0].Canvas.Width = 1
			again, err := verified.Images()
			if err != nil || again[0].Canvas.Width != 100 {
				t.Fatal("images mutable")
			}
		})
	}
}
func TestMultiOriginalDocumentsRecheckAllLocators(t *testing.T) {
	for _, field := range []string{"source_version", "array_version", "root_version", "binding"} {
		t.Run(field, func(t *testing.T) {
			binding, task, inv, raw := originalInventoryFixture(t, "optical.panorama")
			source := &multiOriginalInventoryTestSource{inventory: inv}
			objects := &multiReadObjects{raw: raw}
			source.after = func(s *multiOriginalInventoryTestSource) {
				switch field {
				case "source_version":
					s.inventory.items[1].Object.VersionID = "changed"
				case "array_version":
					s.inventory.items[0].Object.VersionID = "changed"
				case "root_version":
					s.inventory.root.VersionID = "changed"
				case "binding":
					s.inventory.binding.Snapshot = uuid.New()
				}
			}
			if _, err := loadOriginalTest(context.Background(), source, objects, binding, task); !errors.Is(err, v2input.ErrExpired) || objects.closed != 2 {
				t.Fatalf("changed authority accepted %v", err)
			}
		})
	}
}
func TestMultiOriginalDocumentsRejectIncompleteManifestAndCloseErrors(t *testing.T) {
	for _, field := range []string{"sha", "close", "extra_child", "foreign_array"} {
		t.Run(field, func(t *testing.T) {
			binding, task, inv, raw := originalInventoryFixture(t, "optical.panorama")
			source := &multiOriginalInventoryTestSource{inventory: inv}
			objects := &multiReadObjects{raw: bytes.Clone(raw)}
			switch field {
			case "sha":
				objects.raw[0] = '['
			case "close":
				objects.closeErr = errors.New("close failed")
			case "extra_child":
				item := source.inventory.items[0]
				item.Role = "InverseField/0"
				item.Ref.ArtifactID = uuid.New()
				item.Object.ContentType = "application/x-npy"
				source.inventory.items = append(source.inventory.items, item)
				sort.Slice(source.inventory.items, func(i, j int) bool {
					a, b := source.inventory.items[i], source.inventory.items[j]
					return a.Ordinal < b.Ordinal || a.Ordinal == b.Ordinal && a.Role < b.Role
				})
			case "foreign_array":
				for i := range source.inventory.items {
					if source.inventory.items[i].Role == "WorkingPixels" {
						source.inventory.items[i].Ref.ArtifactID = uuid.New()
					}
				}
			}
			if _, err := loadOriginalTest(context.Background(), source, objects, binding, task); err == nil {
				t.Fatal("invalid original accepted")
			}
			if objects.opened != objects.closed {
				t.Fatal("unclosed original stream")
			}
			if source.calls != 1 {
				t.Fatal("invalid original reached final authority read")
			}
		})
	}
}
