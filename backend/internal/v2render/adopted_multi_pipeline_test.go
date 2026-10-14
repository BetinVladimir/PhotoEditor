package v2render

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"strings"
	"testing"
)

type multiPipelineSource struct {
	*multiReadSource
	*multiOriginalInventoryTestSource
	children map[string]multiChildPin
}

func (s *multiPipelineSource) LoadChildObject(_ context.Context, _ auth.Identity, _, _, _ uuid.UUID, op string, root, ref v2input.Ref, path, contract, bucket string) (objectstore.Object, error) {
	pin, ok := s.children[path+"/"+ref.ArtifactID.String()]
	if !ok || op != s.binding.Operation || root != s.binding.Result || bucket != "photos" || pin.ref != ref || pin.contract != contract {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return pin.object, nil
}
func multiPipelineFixture(t *testing.T, op, bad string) (*multiPipelineSource, *adoptedArrayObjects, uuid.UUID) {
	t.Helper()
	v, task, inventory, objects := adoptedArrayFixture(t, op, adoptedArrayNPY("(100,100,3)", 3, 2), adoptedArrayNPY("(100,100)", 1, .5))
	cylindrical := strings.HasPrefix(bad, "cylindrical")
	if cylindrical {
		params, err := v2optical.ParsePanoramaParams([]byte(v.binding.ParamsJCS))
		if err != nil {
			t.Fatal(err)
		}
		params.Projection = "cylindrical"
		raw, _, err := params.Compile()
		if err != nil {
			t.Fatal(err)
		}
		v.binding.ParamsJCS = string(raw)
		inventory.inventory.binding = v.binding
	}
	for _, item := range v.inventory.items {
		if item.Role == "WorkingImage" {
			objects.bodies[item.Object.Key] = []byte(v.documents[item.Ordinal])
		}
	}
	source := &multiPipelineSource{multiOriginalInventoryTestSource: inventory, children: map[string]multiChildPin{}}
	stage := func(raw []byte, key, path, contract, media string) v2input.Ref {
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: adoptedArraySHA(raw)}
		object := objectstore.Object{Key: key, VersionID: "exact-version", SHA256: ref.SHA256, ContentType: media, Bytes: int64(len(raw))}
		objects.bodies[key] = raw
		source.children[path+"/"+ref.ArtifactID.String()] = multiChildPin{ref: ref, path: path, contract: contract, object: object}
		return ref
	}
	marshal := func(value any) []byte {
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	pixels := adoptedArrayNPY("(100,100,3)", 3, -3)
	alpha := adoptedArrayNPY("(100,100)", 1, .5)
	changed := adoptedArrayNPY("(100,100)", 1, 1)
	switch bad {
	case "shape":
		pixels = adoptedArrayNPY("(50,200,3)", 3, -3)
	case "alpha":
		alpha = adoptedArrayNPY("(100,100)", 1, 1.1)
	case "coverage", "cylindrical_coverage":
		changed = adoptedArrayNPY("(100,100)", 1, 0)
	case "mask":
		changed = adoptedArrayNPY("(100,100)", 1, .5)
	}
	rgbRef := stage(pixels, "output/pixels", "$/working_image/pixels", "WorkingPixels", "application/x-npy")
	alphaRef := stage(alpha, "output/alpha", "$/working_image/alpha", "WorkingAlpha", "application/x-npy")
	maskPixels := stage(changed, "output/mask", "$/changed_mask/artifact", "MaskPixels", "application/x-npy")
	images, err := v.Images()
	if err != nil {
		t.Fatal(err)
	}
	frames, reference, _, err := v.binding.Inputs()
	if err != nil {
		t.Fatal(err)
	}
	image := images[reference]
	from := image.Canvas
	image.Canvas.SpaceID = "multi:pipeline"
	image.Pixels = review.ArtifactRef{ID: rgbRef.ArtifactID, SHA256: rgbRef.SHA256}
	image.Alpha = &review.ArtifactRef{ID: alphaRef.ArtifactID, SHA256: alphaRef.SHA256}
	matrix := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	if bad == "coverage" || bad == "geometry_changed" {
		matrix[2] = 1
	}
	fields := make([]any, len(frames))
	if cylindrical {
		for i, frame := range frames {
			raw := cylindricalPipelineNPY()
			if bad == "cylindrical_bad_field" && i == 0 {
				binary.LittleEndian.PutUint32(raw[len(raw)-80000+((50*100+50)*2)*4:], math.Float32bits(123))
			}
			field := stage(raw, "output/field/"+frame.AssetVersionID.String(), "$/report/registrations/*/inverse_field", "DenseWarpField", "application/x-npy")
			fields[i] = field
		}
	}
	seed, err := canonical.Hash(map[string]any{"operation": op, "reference": reference, "sources": frames, "matrices": [][]float64{matrix, matrix}, "inverse_fields": fields, "height": 100, "width": 100})
	if err != nil {
		t.Fatal(err)
	}
	image.Canvas.SpaceID = "multi:" + seed[:32]
	steps := []any{}
	if cylindrical {
		field := fields[reference].(v2input.Ref)
		sha, err := canonical.Hash(map[string]any{"canvas": from, "field": field})
		if err != nil {
			t.Fatal(err)
		}
		projection := from
		projection.SpaceID = "cylindrical:" + sha[:32]
		steps = append(steps, map[string]any{"kind": "dense_warp", "from_canvas": from, "to_canvas": projection, "invertibility": "partial", "valid_region": map[string]any{"x": 0, "y": 0, "width": 100, "height": 100}, "inverse_field": field, "pixel_centers": "half_pixel"})
		from = projection
	}
	step := map[string]any{"kind": "matrix", "from_canvas": from, "to_canvas": image.Canvas, "invertibility": "partial", "valid_region": map[string]any{"x": 0, "y": 0, "width": 100, "height": 100}, "forward_matrix": matrix}
	steps = append(steps, step)
	chain := map[string]any{"steps": steps}
	hash, err := canonical.Hash(chain)
	if err != nil {
		t.Fatal(err)
	}
	chain["hash"] = hash
	image.TransformChain = marshal(chain)
	imageRef := stage(marshal(image), "output/image", "$/working_image", "WorkingImage", "application/json")
	mask := review.MaskRef{Artifact: review.ArtifactRef{ID: maskPixels.ArtifactID, SHA256: maskPixels.SHA256}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "changed", Confidence: 1}
	maskRef := stage(marshal(mask), "output/mask-document", "$/changed_mask", "MaskRef", "application/json")
	target, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	regs := make([]v2input.Ref, len(frames))
	for i, frame := range frames {
		var field *v2input.Ref
		if cylindrical {
			value := fields[i].(v2input.Ref)
			field = &value
		}
		regs[i] = stage(marshal(MultiRegistration{SchemaVersion: 1, Source: frame, ToCanvasHash: target, Homography: matrix, InverseField: field}), "output/registration/"+frame.AssetVersionID.String(), "$/report/registrations/*", "Artifact", "application/json")
	}
	report := MultiImageReport{SchemaVersion: 1, Status: "review", Operation: op, Reason: strings.ToUpper(strings.TrimPrefix(op, "optical.")) + "_REVIEW;motion_fraction=0", Sources: frames, Registrations: regs}
	reportRef := stage(marshal(report), "output/report", "$/report", "ImageReport", "application/json")
	rootRaw := marshal(v2draft.ImageResultDocument{SchemaVersion: 1, WorkingImage: imageRef, ChangedMask: maskRef, Report: reportRef})
	binding := v.binding
	binding.Result.SHA256 = adoptedArraySHA(rootRaw)
	source.multiReadSource = multiReaderSource(binding, rootRaw)
	source.inventory.binding = binding
	source.inventory.root = source.object
	objects.bodies[source.object.Key] = rootRaw
	return source, objects, task
}
func TestAdoptedMultiCompleteReadPipeline(t *testing.T) {
	for _, op := range []string{"optical.panorama", "optical.focus_stack"} {
		for _, bad := range []string{"valid", "geometry_changed", "coverage", "shape", "alpha", "mask", "sha", "late-json-version"} {
			t.Run(op+"/"+bad, func(t *testing.T) {
				source, objects, task := multiPipelineFixture(t, op, bad)
				who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
				tenant, project := uuid.New(), uuid.New()
				docs, err := LoadAdoptedMultiDocuments(context.Background(), source, objects, who, tenant, project, task, op, source.binding.Result, "photos")
				if err != nil {
					t.Fatal(err)
				}
				if bad == "sha" {
					body := append([]byte(nil), objects.bodies["output/pixels"]...)
					body[len(body)-1] ^= 1
					objects.bodies["output/pixels"] = body
				}
				if bad == "late-json-version" {
					objects.before = func(obj objectstore.Object) {
						if obj.Key == "output/pixels" {
							for key, pin := range source.children {
								if pin.path == "$/report" {
									pin.object.VersionID = "replacement"
									source.children[key] = pin
								}
							}
						}
					}
				}
				err = VerifyAdoptedMultiOutputArrays(context.Background(), source, objects, who, tenant, project, task, docs, "photos")
				if bad == "valid" || bad == "geometry_changed" {
					if err != nil {
						t.Fatal(err)
					}
				} else if bad == "late-json-version" {
					if !errors.Is(err, v2input.ErrExpired) {
						t.Fatalf("version mutation: %v", err)
					}
				} else if !errors.Is(err, v2input.ErrInvalid) {
					t.Fatalf("%s accepted: %v", bad, err)
				}
				if objects.opened != objects.closed {
					t.Fatalf("stream leak %d/%d", objects.opened, objects.closed)
				}
				if (bad == "valid" || bad == "geometry_changed") && objects.opened != 15 {
					t.Fatalf("pipeline skipped reads: %d", objects.opened)
				}
			})
		}
	}
}

func TestMultiContentCannotReturnPartialOutputProof(t *testing.T) {
	for _, bad := range []string{"valid", "mask"} {
		t.Run(bad, func(t *testing.T) {
			s, o, task := multiPipelineFixture(t, "optical.focus_stack", bad)
			content, err := LoadAdoptedMultiContent(context.Background(), s, o, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, s.binding.Operation, s.binding.Result, "photos")
			if bad == "valid" || bad == "geometry_changed" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := content.Image(); err != nil {
					t.Fatal(err)
				}
				report, err := content.Report()
				if err != nil || report.Status != "review" {
					t.Fatal("lost review obligation")
				}
			} else {
				if !errors.Is(err, v2input.ErrInvalid) {
					t.Fatal(err)
				}
				if _, err := content.Binding(); !errors.Is(err, v2input.ErrInvalid) {
					t.Fatal("partial proof escaped")
				}
			}
			if o.opened != o.closed {
				t.Fatal("stream leak")
			}
		})
	}
	var empty MultiResultContent
	if _, err := empty.Image(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero image accepted")
	}
	if _, err := empty.Mask(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero mask accepted")
	}
	if _, err := empty.Report(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero report accepted")
	}
}

// Actual half-pixel fields, with float32 conversion performed only on storage.
func cylindricalPipelineNPY() []byte {
	raw := adoptedArrayNPY("(100,100,2)", 2, 0)
	offset := len(raw) - 100*100*2*4
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			theta := (float64(x) + .5 - 50) / 100
			sx := 100*math.Tan(theta) + 50
			sy := (float64(y)+.5-50)/math.Cos(theta) + 50
			if sx >= .5 && sx <= 99.5 && sy >= .5 && sy <= 99.5 {
				binary.LittleEndian.PutUint32(raw[offset+(y*100+x)*8:], math.Float32bits(float32(sx)))
				binary.LittleEndian.PutUint32(raw[offset+(y*100+x)*8+4:], math.Float32bits(float32(sy)))
			}
		}
	}
	return raw
}
func TestCylindricalCompleteMultiContentPipeline(t *testing.T) {
	for _, kind := range []string{"cylindrical", "cylindrical_bad_field", "cylindrical_coverage", "cylindrical_late_field_version"} {
		t.Run(kind, func(t *testing.T) {
			s, o, task := multiPipelineFixture(t, "optical.panorama", kind)
			if kind == "cylindrical_late_field_version" {
				count := 0
				o.before = func(obj objectstore.Object) {
					if strings.HasPrefix(obj.Key, "output/field/") {
						count++
						if count == 2 {
							for key, pin := range s.children {
								if pin.object.Key != obj.Key && pin.contract == "DenseWarpField" {
									pin.object.VersionID = "replacement"
									s.children[key] = pin
								}
							}
						}
					}
				}
			}
			content, err := LoadAdoptedMultiContent(context.Background(), s, o, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, s.binding.Operation, s.binding.Result, "photos")
			switch kind {
			case "cylindrical":
				if err != nil {
					t.Fatal(err)
				}
				if _, err := content.Image(); err != nil {
					t.Fatal(err)
				}
				if o.opened != 17 {
					t.Fatalf("missing projection reads: %d", o.opened)
				}
			case "cylindrical_bad_field", "cylindrical_coverage":
				if !errors.Is(err, v2input.ErrInvalid) {
					t.Fatalf("field equation: %v", err)
				}
			case "cylindrical_late_field_version":
				if !errors.Is(err, v2input.ErrExpired) {
					t.Fatalf("late field: %v", err)
				}
			}
			if o.opened != o.closed {
				t.Fatal("stream leak")
			}
		})
	}
}
