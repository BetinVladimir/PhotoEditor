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
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type signingOriginalQualifierFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, v2input.Ref, int64) error

func (f signingOriginalQualifierFunc) QualifySigningOriginal(ctx context.Context, w auth.Identity, r ReadySigningExport, e CanonicalSigningTask, o, m v2input.Ref, size int64) error {
	return f(ctx, w, r, e, o, m, size)
}

func TestDecodeClaimsUsePhysicalOriginalAsOnlySourceRoot(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("invalid fixture")
	}
	for _, mode := range []string{"valid", "alpha", "suffix", "analyze_alpha", "source_sha", "space", "orientation", "icc", "pixels", "input", "original", "changed_document", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			originalBody := []byte("physical encoded original")
			original := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(originalBody)}
			image, err := review.ParseWorkingImage(images[0])
			if err != nil {
				t.Fatal(err)
			}
			workICC := image.Color.ICCSHA256
			image.Canvas.SourceSHA256 = original.SHA256
			image.Canvas.SpaceID = "oriented:" + original.SHA256
			image.Canvas.Orientation = 1
			if mode == "alpha" || mode == "analyze_alpha" {
				image.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			}
			outputs := []v2input.Ref{{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}}
			if image.Alpha != nil {
				outputs = append(outputs, v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256})
			}
			switch mode {
			case "source_sha":
				image.Canvas.SourceSHA256 = strings.Repeat("f", 64)
			case "space":
				image.Canvas.SpaceID = "foreign"
			case "orientation":
				image.Canvas.Orientation = 8
			case "icc":
				workICC = strings.Repeat("b", 64)
			case "pixels":
				image.Pixels.ID = uuid.New()
			}
			imageRaw, master := contractBytes(t, image)
			outputs = append(outputs, master)
			p := signingDecodeParams{Source: original, Bytes: int64(len(originalBody)), Suffix: ".png", Tile: 512}
			if mode == "suffix" {
				p.Suffix = ".exe"
			}
			if mode == "analyze_alpha" {
				p.Analyze = true
			}
			paramsRaw, paramsRef := contractBytes(t, p)
			inputs := []v2input.Ref{original}
			if mode == "input" {
				inputs[0] = master
			}
			inputRaw, _ := canonical.Marshal(inputs)
			outputRaw, _ := canonical.Marshal(outputs)
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Task: uuid.New(), Run: uuid.New()}
			e := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "asset.decode", Version: "1", Params: paramsRef, Inputs: inputRaw, Outputs: outputRaw, Metrics: json.RawMessage(`{}`), InputDigest: strings.Repeat("c", 64), ResultDigest: strings.Repeat("d", 64)}
			calls := map[v2input.Ref]int{}
			reader := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, r v2input.Ref, _ int) ([]byte, error) {
				calls[r]++
				var raw []byte
				if r == paramsRef {
					raw = paramsRaw
				} else if r == master {
					raw = imageRaw
				} else {
					return nil, ErrInvalid
				}
				if mode == "changed_document" && calls[r] > 1 {
					return []byte(`{}`), nil
				}
				return append([]byte(nil), raw...), nil
			})
			qualified := 0
			qualifier := signingOriginalQualifierFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, o, m v2input.Ref, size int64) error {
				qualified++
				if o != original || m != master || size != int64(len(originalBody)) || mode == "original" {
					return ErrInvalid
				}
				return nil
			})
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := (PresentationSigningTaskResolver{Documents: reader, Originals: qualifier, WorkICCSHA: workICC}).ResolveSigningTaskLineage(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, e)
			if mode == "valid" || mode == "alpha" {
				if err != nil || qualified != 1 || len(result.Sources) != 1 || result.Sources[0] != original || len(result.Node.Parents) != 0 || len(result.Node.Inputs) != 1 || result.Node.Inputs[0] != original || result.Node.Run != e.Run || result.Node.Action.Generated {
					t.Fatal("original identity lost", err)
				}
				body, err := CompileSigningLineage(ctx, SigningExecutionGraph{FinalTask: e.Task, Export: master, Sources: result.Sources, Nodes: []SigningExecutionNode{result.Node}})
				if err != nil {
					t.Fatal(err)
				}
				lineage, err := DecodeSigningLineage(body, v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(body)})
				if err != nil || len(lineage.SourceHashes) != 1 || lineage.SourceHashes[0] != original.SHA256 {
					t.Fatal("document SHA substituted for original", err)
				}
			} else if err == nil || result.Node.Task != uuid.Nil {
				t.Fatal("invalid decode provenance exposed")
			}
		})
	}
}

func TestDecodeClaimsValidateDocumentedParamsDefaults(t *testing.T) {
	original := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	for _, mode := range []string{"valid", "raw", "null_bool", "unknown", "tile", "size", "suffix", "profile"} {
		t.Run(mode, func(t *testing.T) {
			fields := map[string]any{"source": original, "expected_bytes": 100, "suffix": ".png"}
			switch mode {
			case "raw":
				fields["suffix"] = ".cr3"
			case "null_bool":
				fields["analyze"] = nil
			case "unknown":
				fields["generated"] = true
			case "tile":
				fields["tile"] = 16
			case "size":
				fields["expected_bytes"] = 536870913
			case "suffix":
				fields["suffix"] = ".exe"
			case "profile":
				fields["assume_profile"] = "unknown"
			}
			raw, _ := canonical.Marshal(fields)
			p, err := decodeSigningDecodeParams(raw)
			if mode == "valid" || mode == "raw" {
				if err != nil || p.Tile != 512 || p.Analyze || p.AssumeProfile != nil {
					t.Fatal("documented defaults refused", err)
				}
			} else if err == nil {
				t.Fatal("invalid decode Params accepted")
			}
		})
	}
}
