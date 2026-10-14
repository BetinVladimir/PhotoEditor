package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRelightOriginalManualPlan(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	sourceRaw, _ := canonical.Marshal(image)
	pin := func(v any) ([]byte, v2input.Ref) {
		raw, e := canonical.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		sha, _ := canonical.Hash(v)
		return raw, v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	}
	p, qualification, reportRaw, calibrationRaw, expected := relightQualificationFixture(t)
	_, p.Base.Source = pin(image)
	p.Base.CanvasHash, _ = canonical.Hash(image.Canvas)
	target := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: p.Depth.SHA256}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1}
	guard := target
	guard.Artifact.ID = uuid.New()
	guard.Protected = true
	targetRaw, targetRef := pin(target)
	guardRaw, guardRef := pin(guard)
	p.Subject = targetRef
	p.Protected = guardRef
	vectors := map[string]any{"version": "manual_mask_v1", "source_sha256": image.Canvas.SourceSHA256, "canvas": image.Canvas, "shapes": []any{}}
	for _, external := range []bool{false, true} {
		vectorRaw, vectorRef := pin(vectors)
		values := map[string]any{"image": p.Base.Source, "vectors": nil, "inline_vectors": vectors, "object_id": nil}
		if external {
			values["vectors"] = vectorRef
			values["inline_vectors"] = nil
		} else {
			vectorRaw = nil
		}
		originalRaw, originalRef := pin(values)
		rootVectors := originalRef
		if external {
			rootVectors = vectorRef
		}
		rootRaw, rootRef := pin(map[string]any{"image": p.Base.Source, "vectors": rootVectors, "target": target, "protected": guard, "target_png": review.ArtifactRef{ID: uuid.New(), SHA256: p.Depth.SHA256}, "protected_png": review.ArtifactRef{ID: uuid.New(), SHA256: p.Depth.SHA256}, "algorithm": "manual_mask_v1"})

		depthRaw, depthRef := pin(review.RelativeDepth{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: p.Depth.SHA256}, Float32Shape: []int64{image.Canvas.Height, image.Canvas.Width}, Canvas: image.Canvas, TransformChain: image.TransformChain, Normalization: "minmax_valid"})
		p.Depth = depthRef
		depthParams, depthParamsRef := pin(map[string]any{"image": p.Base.Source, "model_id": "depth_anything_v2_small", "model_lock_id": p.Depth.SHA256, "preprocessing_id": "depth_anything_v2_small_518_v1", "input_size": 518})
		documents := RelightInputDocuments{Source: sourceRaw, Depth: depthRaw, Subject: targetRaw, Protected: guardRaw}
		producers := RelightProducerDocuments{Depth: RelightDepthProducer{Ref: depthParamsRef, Body: depthParams}, Manual: []RelightManualProducer{{Root: rootRef, Original: originalRef, RootBody: rootRaw, ParamsBody: originalRaw, VectorsBody: vectorRaw}}}
		paramsRaw, _, err := p.Compile()
		if err != nil {
			t.Fatal(err)
		}
		capacity, err := LocalPortraitResources(image.Canvas.Width, image.Canvas.Height)
		if err != nil {
			t.Fatal(err)
		}
		full, err := CompileRelightFullInputPlan(context.Background(), p.Base, documents, producers, paramsRaw, qualification.RuntimeLockSHA, capacity, qualification)
		if err != nil {
			t.Fatal("full producer inventory", err)
		}
		want := 13
		if external {
			want++
		}
		if len(full.Inputs) != want {
			t.Fatal("full closure count", len(full.Inputs), want)
		}
		intent, err := CompileRelightProducerIntent(context.Background(), uuid.New(), 0, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, p.Base, documents, producers, paramsRaw, qualification.RuntimeLockSHA, capacity, qualification, RelightQualificationEvidence{Report: reportRaw, Calibration: calibrationRaw}, expected)
		encoded, _ := canonical.Marshal(full.Inputs)
		if err != nil || intent.InputsJCS != string(encoded) {
			t.Fatal("full intent", err)
		}
		for _, mode := range []string{"depth", "manual", "duplicate", "bytes", "alias", "extra-person"} {
			bad := producers
			bad.Manual = append([]RelightManualProducer{}, producers.Manual...)
			switch mode {
			case "depth":
				bad.Depth = RelightDepthProducer{}
			case "manual":
				bad.Manual = nil
			case "duplicate":
				bad.Manual = append(bad.Manual, bad.Manual[0])
			case "bytes":
				bad.Manual[0].ParamsBody = append(append([]byte{}, originalRaw...), ' ')
			case "alias":
				bad.Depth.Ref.ArtifactID = p.Depth.ArtifactID
			case "extra-person":
				person := producers.Depth
				bad.Person = &person
			}
			denied, err := CompileRelightFullInputPlan(context.Background(), p.Base, documents, bad, paramsRaw, qualification.RuntimeLockSHA, capacity, qualification)
			if err == nil || denied.Inputs != nil {
				t.Fatal("invalid producer closure", mode)
			}
		}
		person := target
		person.Selector = "person"
		person.Artifact.ID = uuid.New()
		personRaw, personRef := pin(person)
		personParams := p
		personParams.Subject = personRef
		personBody, personOriginal := pin(map[string]any{"image": p.Base.Source, "model_id": "lraspp_mobilenet_v3_large", "model_lock_id": p.Depth.SHA256, "preprocessing_id": "person_guided_v1", "long_side": 768, "guided_tile": 512})
		withPerson := producers
		withPerson.Person = &RelightPersonProducer{Ref: personOriginal, Body: personBody}
		personDocuments := documents
		personDocuments.Subject = personRaw
		personOperation, _, _ := personParams.Compile()
		if _, err := CompileRelightFullInputPlan(context.Background(), p.Base, personDocuments, withPerson, personOperation, qualification.RuntimeLockSHA, capacity, qualification); err != nil {
			t.Fatal("person and manual guard", err)
		}
		withPerson.Person = nil
		if _, err := CompileRelightFullInputPlan(context.Background(), p.Base, personDocuments, withPerson, personOperation, qualification.RuntimeLockSHA, capacity, qualification); err == nil {
			t.Fatal("missing person producer")
		}
		for _, protected := range []bool{false, true} {
			maskRaw := targetRaw
			if protected {
				maskRaw = guardRaw
			}
			plan, err := ResolveRelightManualPlan(context.Background(), p, sourceRaw, maskRaw, originalRaw, rootRaw, vectorRaw, originalRef, rootRef, protected)
			if err == nil {
				checkRelightManualPixels(t, plan)
			}
			if err != nil || plan.Canvas() != image.Canvas {
				t.Fatal(external, protected, err)
			}
			for _, mode := range []string{"role", "bytes", "source"} {
				badP := p
				badMask := maskRaw
				badRole := protected
				if mode == "role" {
					badRole = !protected
				}
				if mode == "bytes" {
					badMask = append(append([]byte{}, maskRaw...), ' ')
				}
				if mode == "source" {
					badP.Base.Source.ArtifactID = uuid.New()
				}
				denied, err := ResolveRelightManualPlan(context.Background(), badP, sourceRaw, badMask, originalRaw, rootRaw, vectorRaw, originalRef, rootRef, badRole)
				if err == nil || denied.CanonicalVectors() != nil {
					t.Fatal("accepted", mode)
				}
			}
		}
	}
}

type relightManualPixelObjects struct {
	body  []byte
	calls int
}

func (o *relightManualPixelObjects) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	o.calls++
	return io.NopCloser(bytes.NewReader(o.body)), nil
}

func checkRelightManualPixels(t *testing.T, plan review.ManualRasterPlan) {
	t.Helper()
	canvas := plan.Canvas()
	header := fmt.Sprintf("{'descr': '<f4', 'fortran_order': False, 'shape': (%d, %d), }", canvas.Height, canvas.Width)
	for (10+len(header)+1)%64 != 0 {
		header += " "
	}
	header += "\n"
	body := append([]byte{147, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}, []byte(header)...)
	binary.LittleEndian.PutUint16(body[8:10], uint16(len(header)))
	body = append(body, make([]byte, 4*canvas.Width*canvas.Height)...)
	digest := sha256.Sum256(body)
	object := objectstore.Object{Key: "target", VersionID: "original", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(body))}
	guard := object
	guard.Key = "guard"
	limits := RelightManualRasterLimits{PayloadBytes: 8 * canvas.Width * canvas.Height}
	for _, mode := range []string{"good", "pixels", "budget", "version"} {
		objects := &relightManualPixelObjects{body: append([]byte{}, body...)}
		bound := limits
		left := object
		right := guard
		if mode == "pixels" {
			objects.body[len(objects.body)-1] = 63
			digest := sha256.Sum256(objects.body)
			left.SHA256 = hex.EncodeToString(digest[:])
			right.SHA256 = left.SHA256
		}
		if mode == "budget" {
			bound.PayloadBytes--
		}
		if mode == "version" {
			right.VersionID = "null"
		}
		err := VerifyRelightManualPixels(context.Background(), objects, plan, left, right, bound)
		if (err == nil) != (mode == "good") {
			t.Fatal(mode, err)
		}
		if (mode == "budget" || mode == "version") && objects.calls != 0 {
			t.Fatal("invalid request reached IO")
		}
	}
}
