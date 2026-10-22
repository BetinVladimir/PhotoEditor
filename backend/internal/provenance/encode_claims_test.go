package provenance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	workflow "photoeditor/backend/internal/encodecontract"
)

type signingDocumentsFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, int) ([]byte, error)

func (f signingDocumentsFunc) ReadSigningTaskDocument(ctx context.Context, w auth.Identity, r ReadySigningExport, e CanonicalSigningTask, ref v2input.Ref, max int) ([]byte, error) {
	return f(ctx, w, r, e, ref, max)
}

type signingProducerFunc func(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (uuid.UUID, error)

func (f signingProducerFunc) ResolveSigningImageProducer(ctx context.Context, w auth.Identity, r ReadySigningExport, ref v2input.Ref) (uuid.UUID, error) {
	return f(ctx, w, r, ref)
}

func TestEncodeSigningClaimsBindSourceAndCanonicalEncode(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("working fixture missing")
	}
	for _, mode := range []string{"valid", "ancestor", "resized", "manifest_dimensions", "manifest_icc", "manifest_artifact", "manifest_hash", "wrong_operation", "review", "mime", "unknown_params", "params_hash", "pixels", "source_hash", "changed_document", "changed_parent", "self_parent", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := canonical.Bytes(images[0])
			if err != nil {
				t.Fatal(err)
			}
			image, err := review.ParseWorkingImage(raw)
			if err != nil {
				t.Fatal(err)
			}
			imageRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(raw)}
			p := signingEncodeParams{Image: imageRef, DownloadPreset: workflow.DownloadPreset{Format: "png", ICCID: "srgb_v1", Subsampling: "444", Alpha: "preserve", MetadataPolicy: "strip"}, ICCSHA256: strings.Repeat("a", 64)}
			if mode == "resized" {
				edge := int64(200)
				p.Size = &workflow.ExportSize{LongEdge: &edge, Fit: "contain"}
			}
			params, paramsRef := contractBytes(t, p)
			if mode == "unknown_params" {
				var extra map[string]any
				json.Unmarshal(params, &extra)
				extra["generated"] = true
				params, paramsRef = contractBytes(t, extra)
			}
			encoded := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
			encodedManifest := signingEncodedImage{Artifact: encoded, Format: p.Format, Width: image.Canvas.Width, Height: image.Canvas.Height, ICCID: p.ICCID, ICCSHA: p.ICCSHA256, Alpha: image.Alpha != nil, Policy: "strip"}
			if mode == "resized" {
				encodedManifest.Width = 200
				encodedManifest.Height = 150
			}
			switch mode {
			case "manifest_dimensions":
				encodedManifest.Width++
			case "manifest_icc":
				encodedManifest.ICCSHA = strings.Repeat("f", 64)
			case "manifest_artifact":
				encodedManifest.Artifact.ArtifactID = uuid.New()
			}
			manifestRaw, manifest := contractBytes(t, encodedManifest)
			if mode == "manifest_hash" {
				manifestRaw = []byte(`{}`)
			}
			inputs := []v2input.Ref{imageRef, {ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}}
			if image.Alpha != nil {
				inputs = append(inputs, v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256})
			}
			if mode == "pixels" {
				inputs[1].ArtifactID = uuid.New()
			}
			inputRaw, _ := canonical.Marshal(inputs)
			outputRaw, _ := canonical.Marshal([]v2input.Ref{encoded, manifest})
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Task: uuid.New(), Run: uuid.New(), Ref: encoded, Object: objectstore.Object{ContentType: "image/png"}}
			e := CanonicalSigningTask{Task: ready.Task, Run: ready.Run, Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "export.encode", Version: "1", Params: paramsRef, InputDigest: strings.Repeat("d", 64), ResultDigest: strings.Repeat("e", 64), Inputs: inputRaw, Outputs: outputRaw, Metrics: json.RawMessage(`{}`)}
			switch mode {
			case "ancestor":
				e.Task = uuid.New()
				e.Run = uuid.New()
				ready.Ref = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
			case "wrong_operation":
				e.Operation = "metadata.write"
			case "review":
				e.ReviewRequired = true
			case "mime":
				ready.Object.ContentType = "image/jpeg"
			case "params_hash":
				params = []byte(`{}`)
			case "source_hash":
				raw = []byte(`{}`)
			}
			documentCalls := map[uuid.UUID]int{}
			producerCalls := 0
			parent := uuid.New()
			documents := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, r v2input.Ref, _ int) ([]byte, error) {
				documentCalls[r.ArtifactID]++
				value := params
				if r == imageRef {
					value = raw
				} else if r == manifest {
					value = manifestRaw
				} else if r != paramsRef {
					return nil, ErrInvalid
				}
				if mode == "changed_document" && documentCalls[r.ArtifactID] > 1 {
					return []byte(`{}`), nil
				}
				return append([]byte(nil), value...), nil
			})
			producers := signingProducerFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, r v2input.Ref) (uuid.UUID, error) {
				producerCalls++
				if r != imageRef {
					return uuid.Nil, ErrInvalid
				}
				if mode == "changed_parent" && producerCalls > 1 {
					return uuid.New(), nil
				}
				if mode == "self_parent" {
					return e.Task, nil
				}
				return parent, nil
			})
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := (PresentationSigningTaskResolver{Documents: documents, Producers: producers}).ResolveSigningTaskLineage(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, e)
			if mode == "valid" || mode == "ancestor" || mode == "resized" {
				if err != nil || len(result.Node.Parents) != 1 || result.Node.Parents[0] != parent || len(result.Node.Inputs) != 1 || result.Node.Inputs[0] != imageRef || result.Node.Action.Generated || result.Node.Action.ModelLockSHA != nil || result.Node.Action.ReviewID != nil || result.Node.Action.ParamsSHA != paramsRef.SHA256 || producerCalls != 2 {
					t.Fatal("encode claims not bound", err)
				}
			} else if err == nil || result.Node.Task != uuid.Nil {
				t.Fatal("invalid encode claims accepted")
			}
		})
	}
}
