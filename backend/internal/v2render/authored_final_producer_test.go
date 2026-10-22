package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredFinalUsesOriginalReportAndActualTargetGeometry(t *testing.T) {
	v, _ := detailMaterializationFixture(t)
	target, err := review.ParseWorkingImage([]byte(v.Current))
	if err != nil {
		t.Fatal(err)
	}
	current := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v.CurrentRef}
	current.CanvasHash, _ = canonical.Hash(target.Canvas)
	original := current
	original.Source.ArtifactID = uuid.New()
	original.CanvasHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	report := func(binding v2input.SessionVersionBinding) []byte {
		raw, _ := canonical.Marshal(map[string]any{"status": "review", "operation": "render.compose", "sources": []v2input.SessionVersionBinding{binding}})
		return raw
	}
	if err := validateAuthoredFinalOutputBinding(target, target, current, original, report(original)); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"styled-report-source", "canvas", "color", "chain", "status", "operation", "missing-source"} {
		t.Run(kind, func(t *testing.T) {
			image := target
			raw := report(original)
			switch kind {
			case "styled-report-source":
				raw = report(current)
			case "canvas":
				image.Canvas.Width++
			case "color":
				image.Color.ICCSHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			case "chain":
				image.TransformChain = json.RawMessage(`{"hash":"bad","steps":[]}`)
			case "status":
				raw = []byte(`{"status":"pass","operation":"render.compose","sources":[]}`)
			case "operation":
				raw = []byte(`{"status":"review","operation":"render.authored_segment","sources":[]}`)
			case "missing-source":
				raw = []byte(`{"status":"review","operation":"render.compose","sources":[]}`)
			}
			if validateAuthoredFinalOutputBinding(image, target, current, original, raw) == nil {
				t.Fatal("foreign final binding accepted")
			}
		})
	}
}
func TestAuthoredFinalCallerCancellationAndMissingAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (AuthoredFinalProducerReader{}).Read(ctx, AuthoredInstructionSource{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (AuthoredFinalProducerReader{}).CommitCanonical(ctx, AuthoredInstructionSource{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (SQLAuthoredFinalContinuation{}).Resume(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (SQLAuthoredFinalContinuation{}).Resume(context.Background(), auth.Identity{}, uuid.Nil, uuid.Nil, 1); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewSQLAuthoredFinalContinuation(nil, nil, nil, "", nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
}

type authoredFinalMissingObject struct{ err error }

func (o authoredFinalMissingObject) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return nil, o.err
}
func TestAuthoredFinalPhysicalArraysStreamAndRejectForeignOutputRoles(t *testing.T) {
	data := bytes.Repeat([]byte("final-array"), 20000)
	ref, pin := authoredSegmentStreamFixture(data)
	for _, role := range []string{"pixels", "alpha", "mask_pixels"} {
		reader := &authoredSegmentStreamReader{data: bytes.NewReader(data)}
		if err := verifyAuthoredFinalBinary(context.Background(), &authoredSegmentStreamObjects{reader: reader}, pin, ref, role); err != nil || reader.closeCount != 1 || reader.maxRead > 65536 {
			t.Fatal(role, err)
		}
	}
	for _, change := range []string{"corrupt", "truncated", "extra", "png", "role", "close", "missing", "cancel"} {
		t.Run(change, func(t *testing.T) {
			p := pin
			body := append([]byte(nil), data...)
			role := "pixels"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := &authoredSegmentStreamReader{}
			sentinel := errors.New("physical-boundary")
			switch change {
			case "corrupt":
				body[0] ^= 1
			case "truncated":
				body = body[:len(body)-1]
			case "extra":
				body = append(body, 0)
			case "png":
				p.ContentType = "image/png"
			case "role":
				role = "transformed_mask_png"
			case "close":
				reader.closeErr = sentinel
			case "cancel":
				reader.cancel = cancel
			}
			reader.data = bytes.NewReader(body)
			var objects v2input.SessionAnalysisObjectReader = &authoredSegmentStreamObjects{reader: reader}
			if change == "missing" {
				objects = authoredFinalMissingObject{err: sentinel}
			}
			err := verifyAuthoredFinalBinary(ctx, objects, p, ref, role)
			if err == nil {
				t.Fatal("physical substitution accepted")
			}
			if change == "close" || change == "missing" {
				if !errors.Is(err, sentinel) {
					t.Fatal(err)
				}
			}
			if change == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
