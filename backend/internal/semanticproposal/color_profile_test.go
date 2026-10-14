package semanticproposal

import (
	"encoding/json"
	"os"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func TestTransferColorRequiresVerifiedRegistryAndExactWorkProfile(t *testing.T) {
	profile, err := LoadTransferColorProfile("../../../resources/icc")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	image.Color.ICCSHA256 = profile.sha
	body, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	if err := profile.VerifyWorkingImage(body); err != nil {
		t.Fatal("qualified work profile", err)
	}
	if err := (TransferColorProfile{}).VerifyWorkingImage(body); err == nil {
		t.Fatal("unqualified profile")
	}
	image.Color.ICCSHA256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	body, _ = canonical.Marshal(image)
	if err := profile.VerifyWorkingImage(body); err == nil {
		t.Fatal("foreign ICC")
	}
	if _, err := LoadTransferColorProfile(t.TempDir()); err == nil {
		t.Fatal("missing registry")
	}
}
