package main

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
	"strings"
	"testing"
)

func TestSemanticCloseBootstrapReplayWithoutPreparation(t *testing.T) {
	t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE", "")
	t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256", "")
	service, err := buildSemanticReviewClose(&pgxpool.Pool{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	apply, ok := service.(*semanticreview.CloseService)
	if !ok || apply.API == nil || apply.Prepare != nil {
		t.Fatal("default bootstrap must install receipt reader only")
	}
	t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE", "/missing")
	if _, err = buildSemanticReviewClose(&pgxpool.Pool{}, nil, ""); err == nil {
		t.Fatal("unpaired config accepted")
	}
}

func TestSemanticCloseBootstrapPinsExactConfigBytes(t *testing.T) {
	raw := []byte(`{"schema_version":1,"review_model_sha":"` + strings.Repeat("a", 64) + `","review_dataset_sha":"` + strings.Repeat("b", 64) + `","document_budget_bytes":67108864}`)
	path := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE", path)
	t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256", hex.EncodeToString(sum[:]))
	service, err := buildSemanticReviewClose(&pgxpool.Pool{}, &objectstore.Store{}, "photos")
	if err != nil {
		t.Fatal(err)
	}
	apply := service.(*semanticreview.CloseService)
	if apply.Prepare == nil {
		t.Fatal("configuration must install close preparation")
	}
	if err = os.WriteFile(path, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = buildSemanticReviewClose(&pgxpool.Pool{}, &objectstore.Store{}, "photos"); err == nil {
		t.Fatal("changed raw config accepted")
	}
}
