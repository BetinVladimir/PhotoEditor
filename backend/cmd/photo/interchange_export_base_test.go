package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestLocalInterchangeExportBaseRequiresIndependentLocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.lock.json")
	sha := func(value string) string {
		digest := sha256.Sum256([]byte(value))
		return hex.EncodeToString(digest[:])
	}
	model := map[string]any{
		"id": "mobilesam", "architecture": "vit", "weight_id": "weights",
		"repository": "release", "revision": "revision1", "preprocessing_id": "rgb",
		"packages_lock_hash": sha("packages"), "code_license": "MIT", "weights_license": "MIT",
		"notices": []any{}, "redistribution_policy": "allowed", "approved_for_install": true,
		"files": []any{map[string]any{"path": "model.pt", "sha256": sha("weight"), "bytes": 6, "url": "https://github.com/model.pt"}},
		"backends": map[string]any{
			"cpu":  map[string]any{"status": "untested", "reason": "pending", "report_sha256": nil},
			"cuda": map[string]any{"status": "untested", "reason": "pending", "report_sha256": nil},
			"mps":  map[string]any{"status": "untested", "reason": "pending", "report_sha256": nil},
		},
	}
	payload := map[string]any{"schema_version": 1, "models": []any{model}}
	lockID, err := canonical.Hash(payload)
	if err != nil {
		t.Fatal(err)
	}
	payload["lock_id"] = lockID
	raw, err := canonical.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	pin := hex.EncodeToString(digest[:])
	operatorDigest := sha256.Sum256([]byte("operator"))
	operatorSHA := hex.EncodeToString(operatorDigest[:])
	base := apiProcessDependencies{}
	if installed, err := installLocalInterchangeExportBase(context.Background(), "", "",
		"", "", "", base); err != nil || installed.InterchangeExportBase != nil {
		t.Fatal("disabled export base changed startup", err)
	}
	for _, inputs := range []struct{ icc, model, pin string }{
		{"", path, pin}, {"../../../resources/icc", "", pin},
		{"../../../resources/icc", path, ""},
	} {
		if installed, err := installLocalInterchangeExportBase(context.Background(),
			"/operator.json", operatorSHA, inputs.icc, inputs.model, inputs.pin, base); err == nil || installed.InterchangeExportBase != nil {
			t.Fatal("partial export base accepted")
		}
	}
	for _, declaration := range []struct{ operator, operatorPin, model, modelPin string }{
		{"relative-operator.json", operatorSHA, path, pin},
		{"/operator.json", "INVALID", path, pin},
		{"/operator.json", operatorSHA, "relative-model.json", pin},
		{"/operator.json", operatorSHA, path, "INVALID"},
	} {
		if installed, err := installLocalInterchangeExportBase(context.Background(),
			declaration.operator, declaration.operatorPin, "../../../resources/icc",
			declaration.model, declaration.modelPin, base); err == nil || installed.InterchangeExportBase != nil {
			t.Fatal("invalid export base path or pin accepted")
		}
	}
	installed, err := installLocalInterchangeExportBase(context.Background(),
		"/operator.json", operatorSHA, "../../../resources/icc", path, pin, base)
	if err != nil || installed.InterchangeExportBase == nil || installed.InterchangeBackgroundFactory == nil ||
		installed.InterchangeExportBase.ModelSHA != lockID {
		t.Fatal("qualified export base unavailable", err)
	}
	if duplicate, err := installLocalInterchangeExportBase(context.Background(),
		"/operator.json", operatorSHA, "../../../resources/icc", path, pin, installed); err == nil || duplicate.InterchangeExportBase == nil {
		t.Fatal("duplicate export base accepted")
	}
}
