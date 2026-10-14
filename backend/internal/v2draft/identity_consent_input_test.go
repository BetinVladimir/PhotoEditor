package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestIdentityConsentInputBindsExactParamsAndBytes(t *testing.T) {
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "grants": []any{}, "confirmed_faces": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}
	object := objectstore.Object{Key: "consents", VersionID: "v1", SHA256: ref.SHA256,
		Bytes: int64(len(raw)), ContentType: "application/json"}
	catalog := &identityInputCatalogFixture{items: map[string]objectstore.Object{"consents": object}, reads: map[string]int{}}
	objects := restorationArrayReaderFixture{"consents": raw}
	inputs := IdentityResultInputs{Params: v2input.IdentityCompareParams{Consents: ref}, ParamsRef: paramsRef}
	scope := IdentityResultScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(),
		Root: v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}}
	got, err := LoadIdentityConsentInput(context.Background(), scope, catalog, objects, inputs)
	if err != nil || got.Ref != ref || got.Object != object || catalog.reads["consents"] != 2 {
		t.Fatalf("valid consent document rejected: %+v %v", got, err)
	}
	objects["consents"] = []byte(`{"schema_version":1,"grants":[],"confirmed_faces":[{}]}`)
	if _, err = LoadIdentityConsentInput(context.Background(), scope, catalog, objects, inputs); err == nil {
		t.Fatal("changed consent bytes accepted")
	}
}
