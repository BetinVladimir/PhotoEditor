package provenance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestMetadataClaimsBindExactBeforeAfterAndReport(t *testing.T) {
	for _, mode := range []string{"valid", "gps", "wrong_operation", "review", "mime", "input", "output", "dimensions", "icc", "report_refs", "copied", "orientation", "previews", "gps_policy", "changed_document", "changed_parent", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			encoded := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
			final := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
			before := signingEncodedImage{Artifact: encoded, Format: "png", Width: 8, Height: 8, ICCID: "srgb_v1", ICCSHA: strings.Repeat("c", 64), Policy: "strip"}
			beforeRaw, beforeRef := contractBytes(t, before)
			author, makeText := "Author", "Camera"
			document := signingMetadataDocument{Author: &author, Make: &makeText, GPS: &signingMetadataGPS{Latitude: 42, Longitude: 23}}
			documentRaw, documentRef := contractBytes(t, document)
			p := signingMetadataParams{Encoded: beforeRef, Document: documentRef, Policy: "copyright_only", ToolID: "exiftool_v1"}
			copied := []string{"author"}
			if mode == "gps" {
				p.Policy = "safe_camera"
				p.CopyGPS = true
				copied = []string{"author", "camera_make", "gps"}
			}
			if mode == "gps_policy" {
				p.CopyGPS = true
			}
			paramsRaw, paramsRef := contractBytes(t, p)
			after := before
			after.Artifact = final
			after.Policy = p.Policy
			switch mode {
			case "dimensions":
				after.Width++
			case "icc":
				after.ICCSHA = strings.Repeat("d", 64)
			}
			afterRaw, afterRef := contractBytes(t, after)
			report := signingMetadataReport{Encoded: beforeRef, Document: documentRef, Policy: p.Policy, Copied: copied, Orientation: 1, PreviewsRemoved: true, ICCPreserved: true}
			switch mode {
			case "report_refs":
				report.Document = beforeRef
			case "copied":
				report.Copied = append(report.Copied, "gps")
			case "orientation":
				report.Orientation = 8
			case "previews":
				report.PreviewsRemoved = false
			}
			reportRaw, reportRef := contractBytes(t, report)
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Task: uuid.New(), Run: uuid.New(), Ref: final, Object: objectstore.Object{ContentType: "image/png"}}
			inputs := []v2input.Ref{beforeRef, encoded, documentRef}
			outputs := []v2input.Ref{final, afterRef, reportRef}
			if mode == "input" {
				inputs[1] = final
			}
			if mode == "output" {
				outputs[0] = encoded
			}
			inputRaw, _ := canonical.Marshal(inputs)
			outputRaw, _ := canonical.Marshal(outputs)
			e := CanonicalSigningTask{Task: ready.Task, Run: ready.Run, Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "metadata.write", Version: "1", Params: paramsRef, Inputs: inputRaw, Outputs: outputRaw, Metrics: json.RawMessage(`{}`), InputDigest: strings.Repeat("e", 64), ResultDigest: strings.Repeat("f", 64)}
			if mode == "wrong_operation" {
				e.Operation = "export.encode"
			}
			if mode == "review" {
				e.ReviewRequired = true
			}
			if mode == "mime" {
				ready.Object.ContentType = "image/jpeg"
			}
			documents := map[v2input.Ref][]byte{paramsRef: paramsRaw, beforeRef: beforeRaw, documentRef: documentRaw, afterRef: afterRaw, reportRef: reportRaw}
			calls := map[v2input.Ref]int{}
			reader := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, r v2input.Ref, _ int) ([]byte, error) {
				calls[r]++
				raw, ok := documents[r]
				if !ok {
					return nil, ErrInvalid
				}
				if mode == "changed_document" && calls[r] > 1 {
					return []byte(`{}`), nil
				}
				return append([]byte(nil), raw...), nil
			})
			parent := uuid.New()
			producerCalls := 0
			producer := signingProducerFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, r v2input.Ref) (uuid.UUID, error) {
				producerCalls++
				if r != beforeRef {
					return uuid.Nil, ErrInvalid
				}
				if mode == "changed_parent" && producerCalls > 1 {
					return uuid.New(), nil
				}
				return parent, nil
			})
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := (PresentationSigningTaskResolver{Documents: reader, Producers: producer}).ResolveSigningTaskLineage(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, e)
			if mode == "valid" || mode == "gps" {
				if err != nil || len(result.Node.Parents) != 1 || result.Node.Parents[0] != parent || len(result.Node.Inputs) != 1 || result.Node.Inputs[0] != beforeRef || result.Node.Action.Generated || result.Node.Action.ModelLockSHA != nil || result.Node.Action.ReviewID != nil || producerCalls != 2 {
					t.Fatal("metadata claims lost", err)
				}
				for r := range documents {
					if calls[r] != 2 {
						t.Fatal("document not rechecked")
					}
				}
			} else if err == nil || result.Node.Task != uuid.Nil {
				t.Fatal("invalid metadata claims exposed")
			}
		})
	}
}

func TestSigningMetadataDocumentLimitsAndFieldSelection(t *testing.T) {
	empty, over, date := "", strings.Repeat("x", 257), "2026-01-01"
	for _, document := range []signingMetadataDocument{{Author: &empty}, {Lens: &over}, {DateTime: &date}, {GPS: &signingMetadataGPS{Latitude: 91}}, {GPS: &signingMetadataGPS{Longitude: -181}}} {
		if _, err := expectedSigningMetadataFields(document, signingMetadataParams{Policy: "safe_camera", CopyGPS: true}); err == nil {
			t.Fatal("invalid metadata document accepted")
		}
	}
	value := "value"
	fields, err := expectedSigningMetadataFields(signingMetadataDocument{Author: &value, Copyright: &value, Make: &value, Model: &value, Lens: &value, GPS: &signingMetadataGPS{}}, signingMetadataParams{Policy: "copyright_only"})
	if err != nil || strings.Join(fields, ",") != "author,copyright" {
		t.Fatal("camera/GPS leaked into copyright policy", err)
	}
}
