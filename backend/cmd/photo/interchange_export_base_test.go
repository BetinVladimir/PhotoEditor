package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalInterchangeExportBaseRequiresIndependentLocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.lock.json")
	raw := []byte(`{"model":"renderer","schema_version":1}`)
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
	installed, err := installLocalInterchangeExportBase(context.Background(),
		"/operator.json", operatorSHA, "../../../resources/icc", path, pin, base)
	if err != nil || installed.InterchangeExportBase == nil || installed.InterchangeBackgroundFactory == nil ||
		installed.InterchangeExportBase.ModelSHA != pin {
		t.Fatal("qualified export base unavailable", err)
	}
	if duplicate, err := installLocalInterchangeExportBase(context.Background(),
		"/operator.json", operatorSHA, "../../../resources/icc", path, pin, installed); err == nil || duplicate.InterchangeExportBase == nil {
		t.Fatal("duplicate export base accepted")
	}
}
