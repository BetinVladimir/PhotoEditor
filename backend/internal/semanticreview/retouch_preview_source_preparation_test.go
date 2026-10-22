package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"photoeditor/backend/internal/v2input"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRetouchSourcePreparationCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, raw, err := LoadRetouchPreviewSourceDocuments(ctx, nil, auth.Identity{}, uuid.New(), uuid.New(), ScopedPreviewReview{}, PreviewRequest{}, nil, ExposureOwnerCosts{}, ExposureResourceEstimate{}, RetouchQualificationPin{})
	if err != context.Canceled || got.TargetID != uuid.Nil || raw != nil {
		t.Fatal("cancelled source escaped", err)
	}
}

func TestRetouchSourcePreparationMissingDependenciesBeforeIO(t *testing.T) {
	got, raw, err := LoadRetouchPreviewSourceDocuments(context.Background(), nil, auth.Identity{}, uuid.New(), uuid.New(), ScopedPreviewReview{}, PreviewRequest{}, nil, ExposureOwnerCosts{}, ExposureResourceEstimate{}, RetouchQualificationPin{})
	if err != ErrInvalid || got.TargetID != uuid.Nil || raw != nil {
		t.Fatal("unscoped source escaped", err)
	}
}

func TestRetouchSourceVersionedRead(t *testing.T) {
	for _, mode := range []string{"ok", "bucket", "key", "version", "null_version", "media", "contract", "ref", "small", "large", "length", "hash", "cancel_before", "cancel_after", "storage"} {
		t.Run(mode, func(t *testing.T) {
			body := []byte(`{"source":1}`)
			sum := sha256.Sum256(body)
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			source := PlanSource{Contract: "WorkingImage", Ref: ref, Bytes: int64(len(body)), Bucket: "owned", Key: "source", Version: "v1", MediaType: "application/json"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "bucket": source.Bucket = ""
			case "key": source.Key = ""
			case "version": source.Version = ""
			case "null_version": source.Version = "null"
			case "media": source.MediaType = "image/png"
			case "contract": source.Contract = "MaskRef"
			case "ref": source.Ref.ArtifactID = uuid.New()
			case "small": source.Bytes = 1
			case "large": source.Bytes = 1048577
			case "length": source.Bytes++
			case "cancel_before": cancel()
			}
			calls := 0
			storageErr := errors.New("storage unavailable")
			got, err := readRetouchPreviewSource(ctx, source, ref, "WorkingImage", func(_ context.Context, pin PlanSource) ([]byte, error) {
				calls++
				if pin != source { t.Fatal("storage pin changed") }
				if mode == "cancel_after" { cancel() }
				if mode == "storage" { return nil, storageErr }
				if mode == "hash" { body[2] = 'X' }
				return body, nil
			})
			if mode == "ok" {
				if err != nil || calls != 1 || string(got) != string(body) { t.Fatal("valid source refused", err) }
				body[0] = 'X'
				if got[0] != '{' { t.Fatal("reader retained storage buffer") }
				return
			}
			if err == nil || got != nil { t.Fatal("invalid source escaped", mode) }
			if mode == "cancel_before" || mode == "cancel_after" {
				if !errors.Is(err, context.Canceled) { t.Fatal(err) }
			} else if mode == "storage" {
				if !errors.Is(err, storageErr) { t.Fatal(err) }
			} else if !errors.Is(err, ErrInvalid) { t.Fatal(err) }
			wantCalls := 0
			if mode == "length" || mode == "hash" || mode == "cancel_after" || mode == "storage" { wantCalls = 1 }
			if calls != wantCalls { t.Fatal("unexpected storage access", calls, wantCalls) }
		})
	}
}
