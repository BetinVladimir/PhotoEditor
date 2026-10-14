package v2portrait

import (
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"testing"
)

func TestDonorConfirmationPinsAndNullableMode(t *testing.T) {
	raw, err := os.ReadFile("testdata/open-eyes-params-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Params OpenEyesParams `json:"params"`
	}
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("fixture")
	}
	p := fixture.Params
	observation := uuid.New()
	donorFace := p.DonorFaceID
	proof := DonorConfirmation{SchemaVersion: 1, PersonID: uuid.New(), Source: p.Base, Donor: &p.Donor, SourceFaceID: p.SourceFaceID, DonorFaceID: &donorFace, SourceObservationID: uuid.New(), DonorObservationID: &observation, ConsentID: uuid.New(), Epoch: "1", ActorID: uuid.New()}
	body, sha, err := proof.Compile()
	if err != nil {
		t.Fatal(err)
	}
	p.Confirmation.SHA256 = sha
	if _, err = VerifyOpenEyesConfirmation(p, body); err != nil {
		t.Fatal(err)
	}
	donorFace = p.SourceFaceID
	changed, sha, err := proof.Compile()
	if err != nil {
		t.Fatal(err)
	}
	p.Confirmation.SHA256 = sha
	if _, err = VerifyOpenEyesConfirmation(p, changed); err == nil {
		t.Fatal("changed face accepted")
	}
	proof.Donor = nil
	if _, _, err = proof.Compile(); err == nil {
		t.Fatal("partial donor accepted")
	}
	proof.DonorFaceID = nil
	proof.DonorObservationID = nil
	body, sha, err = proof.Compile()
	if err != nil {
		t.Fatal("nullable generative schema rejected")
	}
	p.Confirmation.SHA256 = sha
	if _, err = VerifyOpenEyesConfirmation(p, body); err == nil {
		t.Fatal("absent donor accepted for open eyes")
	}
	proof.Epoch = "01"
	if _, _, err = proof.Compile(); err == nil {
		t.Fatal("invalid epoch accepted")
	}
}
