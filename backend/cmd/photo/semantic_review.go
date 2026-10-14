package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
)

// Replay remains available without preparation configuration. This installation
// enables rejection only; approved admission requires its own trusted contour.
func buildSemanticReviewApply(api *pgxpool.Pool, objects *objectstore.Store, bucket string) (httpapi.SemanticReviewApply, error) {
	if api == nil {
		return nil, errors.New("semantic review database unavailable")
	}
	service := &semanticreview.ApplyService{API: api}
	path, expected := os.Getenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE"), os.Getenv("PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256")
	if path == "" && expected == "" {
		return service, nil
	}
	if path == "" || len(expected) != 64 {
		return nil, errors.New("semantic review configuration invalid")
	}
	digest, err := hex.DecodeString(expected)
	if err != nil || hex.EncodeToString(digest) != expected {
		return nil, errors.New("semantic review configuration invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("semantic review configuration unavailable")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 16385))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) > 16384 {
		return nil, errors.New("semantic review configuration unavailable")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != expected {
		return nil, errors.New("semantic review configuration digest mismatch")
	}
	config, err := semanticreview.DecodeReviewPreparationConfig(raw)
	if err != nil || objects == nil {
		return nil, errors.New("semantic review configuration invalid")
	}
	read, err := semanticreview.NewPlanObjectReader(objects, bucket)
	if err != nil {
		return nil, errors.New("semantic review object reader unavailable")
	}
	service.Prepare, err = semanticreview.NewRejectionPreparation(api, objects, read, bucket, config.DocumentBudgetBytes, config.ModelSHA, config.DatasetSHA)
	if err != nil {
		return nil, errors.New("semantic review preparation unavailable")
	}
	return service, nil
}
