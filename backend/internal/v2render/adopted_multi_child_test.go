package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiChildExactTypedPaths(t *testing.T) {
	cases := map[string][]string{"WorkingImage": {"$/working_image"}, "ImageReport": {"$/report"}, "MaskRef": {"$/changed_mask"}, "Artifact": {"$/report/registrations/*"}, "WorkingPixels": {"$/working_image/pixels"}, "WorkingAlpha": {"$/working_image/alpha"}, "MaskPixels": {"$/changed_mask/artifact"}, "DenseWarpField": {"$/working_image/transform_chain/steps/*/inverse_field", "$/changed_mask/transform_chain/steps/*/inverse_field", "$/report/registrations/*/inverse_field"}}
	for contract, paths := range cases {
		for _, path := range paths {
			media, limit, ok := multiChildObjectLimit(path, contract)
			if !ok || media == "" || limit < 1 {
				t.Fatal("missing typed edge", path, contract)
			}
			for _, wrong := range []string{path + "/other", "$/../../report", "$/report/registrations/0", "$/generated_mask", "$/report/transform"} {
				if _, _, ok := multiChildObjectLimit(wrong, contract); ok {
					t.Fatal("unscoped path", wrong)
				}
			}
		}
	}
	for _, contract := range []string{"Params", "Calibration", "IdentityReport", "ImageResult", "RegisteredTransform", "NpyArray"} {
		if _, _, ok := multiChildObjectLimit("$/report/registrations/*", contract); ok {
			t.Fatal("unscoped contract", contract)
		}
	}
}
func TestMultiOriginalRoleLimits(t *testing.T) {
	for _, role := range []string{"WorkingImage", "WorkingPixels", "WorkingAlpha", "InverseField/0", "InverseField/127"} {
		media, limit, legacy, ok := multiOriginalObjectLimit(role)
		if !ok || media == "" || limit < 1 {
			t.Fatal(role)
		}
		if legacy != (role == "WorkingPixels" || role == "WorkingAlpha") {
			t.Fatal("legacy media widened", role)
		}
	}
	for _, role := range []string{"NpyArray", "Artifact", "Params", "InverseField/128", "InverseField/00", "InverseField/+1", "InverseField/-1", "InverseField/1e2", "InverseField/1/other"} {
		if _, _, _, ok := multiOriginalObjectLimit(role); ok {
			t.Fatal("unscoped original role", role)
		}
	}
}

type multiJSONChildSource struct {
	*multiReadSource
	child      objectstore.Object
	childCalls int
	afterChild func(*multiJSONChildSource)
}

func (s *multiJSONChildSource) LoadChildObject(_ context.Context, _ auth.Identity, _, _, _ uuid.UUID, _ string, _, _ v2input.Ref, _, _, _ string) (objectstore.Object, error) {
	s.childCalls++
	if s.childCalls > 1 && s.afterChild != nil {
		s.afterChild(s)
	}
	return s.child, nil
}
func multiJSONChildFixture(t *testing.T) (AdoptedMultiBinding, uuid.UUID, *multiJSONChildSource, *multiReadObjects, v2input.Ref) {
	t.Helper()
	binding, task, root := multiReadFixture(t, "optical.panorama")
	frames, _, _, _ := binding.Inputs()
	// Byte loader does not certify contract semantics; catalogue parsing follows.
	raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "status": "review", "operation": binding.Operation, "skipped_count": 0, "reason": "PANORAMA_REVIEW;motion_fraction=0", "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": nil, "transform": nil, "sources": frames, "registrations": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	source := &multiJSONChildSource{multiReadSource: multiReaderSource(binding, root), child: objectstore.Object{Key: "adopted/report", VersionID: "report-version", SHA256: ref.SHA256, ContentType: "application/json", Bytes: int64(len(raw))}}
	return binding, task, source, &multiReadObjects{raw: raw}, ref
}
func readMultiChildTest(ctx context.Context, binding AdoptedMultiBinding, task uuid.UUID, source *multiJSONChildSource, objects *multiReadObjects, ref v2input.Ref) ([]byte, error) {
	return LoadAdoptedMultiJSONChild(ctx, source, objects, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, binding, ref, "$/report", "ImageReport", "photos")
}
func TestMultiJSONChildVerifiesBytesAndRepeatsAuthority(t *testing.T) {
	binding, task, source, objects, ref := multiJSONChildFixture(t)
	body, err := readMultiChildTest(context.Background(), binding, task, source, objects, ref)
	if err != nil || !bytes.Equal(body, objects.raw) || source.calls != 2 || source.childCalls != 2 || objects.closed != 1 {
		t.Fatalf("child read %v %+v", err, source)
	}
	body[0] = '['
	if objects.raw[0] != '{' {
		t.Fatal("returned child aliases object bytes")
	}
}
func TestMultiJSONChildRefusesChangedLocatorAndRoot(t *testing.T) {
	for _, field := range []string{"child_version", "child_length", "root_version", "root_binding"} {
		t.Run(field, func(t *testing.T) {
			binding, task, source, objects, ref := multiJSONChildFixture(t)
			switch field {
			case "child_version":
				source.afterChild = func(s *multiJSONChildSource) { s.child.VersionID = "changed" }
			case "child_length":
				source.afterChild = func(s *multiJSONChildSource) { s.child.Bytes++ }
			case "root_version":
				source.after = func(s *multiReadSource) { s.object.VersionID = "changed" }
			case "root_binding":
				source.after = func(s *multiReadSource) { s.binding.Snapshot = uuid.New() }
			}
			if _, err := readMultiChildTest(context.Background(), binding, task, source, objects, ref); !errors.Is(err, v2input.ErrExpired) || objects.closed != 1 {
				t.Fatalf("changed child accepted %v", err)
			}
		})
	}
}
func TestMultiJSONChildClosesInvalidBytes(t *testing.T) {
	for _, field := range []string{"sha", "extra", "close", "cancel"} {
		t.Run(field, func(t *testing.T) {
			binding, task, source, objects, ref := multiJSONChildFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch field {
			case "sha":
				objects.raw[0] = '['
			case "extra":
				objects.raw = append(objects.raw, ' ')
			case "close":
				objects.closeErr = errors.New("close failed")
			case "cancel":
				objects.before = cancel
			}
			if _, err := readMultiChildTest(ctx, binding, task, source, objects, ref); err == nil || objects.closed != 1 || source.childCalls != 1 {
				t.Fatalf("bad child accepted %v", err)
			}
		})
	}
}
func TestMultiChildMetadataRefusesLegacyOutputAndInvalidVersion(t *testing.T) {
	_, _, source, _, ref := multiJSONChildFixture(t)
	obj := source.child
	obj.ContentType = "application/octet-stream"
	if validMultiChildObject(obj, ref, "application/x-npy", 65536, false) {
		t.Fatal("legacy output media accepted")
	}
	if !validMultiChildObject(obj, ref, "application/x-npy", 65536, true) {
		t.Fatal("explicit original legacy media refused")
	}
	obj.VersionID = "null"
	if validMultiChildObject(obj, ref, "application/x-npy", 65536, true) {
		t.Fatal("unversioned input accepted")
	}
}
