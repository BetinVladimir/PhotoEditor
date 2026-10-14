package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestGlareDonorIssuerUsesExactOpenEyesConsentScope(t *testing.T) {
	raw, err := os.ReadFile("testdata/open-eyes-params-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Params OpenEyesParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	open := fixture.Params
	request := GlareDonorIssuerParams{Base: open.Base, Donor: open.Donor,
		Strength: open.Strength, GlassesMask: open.EyesMask,
		Protected: open.Protected, Confirmation: open.Confirmation,
		SourceFaces: open.SourceFaces, DonorFaces: open.DonorFaces,
		SourceFaceID: open.SourceFaceID, DonorFaceID: open.DonorFaceID}
	bound, err := request.openEyesParams()
	if err != nil || bound != open {
		t.Fatal("donor glare issuer changed trusted consent scope", err)
	}
	request.DonorFaceID = uuid.Nil
	if _, err := request.openEyesParams(); err == nil {
		t.Fatal("missing donor face accepted")
	}
	request.DonorFaceID = open.DonorFaceID
	request.GlassesMask = request.Protected
	if _, err := request.openEyesParams(); err == nil {
		t.Fatal("aliased mask roots accepted")
	}
	request.GlassesMask = open.EyesMask
	request.Confirmation = request.SourceFaces
	if _, err := request.openEyesParams(); err == nil {
		t.Fatal("confirmation aliased to observations")
	}
}

func TestGlareDonorPreparationProofRejectsUnscopedRequestBeforeDatabase(t *testing.T) {
	got, err := ReadGlareDonorPreparationProof(context.Background(), nil,
		auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(),
		uuid.New(), uuid.New(), uuid.New(), []byte(`{}`), []byte(`{}`),
		uuid.New(), uuid.New())
	if err != v2input.ErrInvalid || len(got.Proof) != 0 {
		t.Fatal("preparation read accepted missing scheduler authority", err)
	}
}

func TestGlareDonorPreparationProofBindsSQLResultBeforeCommit(t *testing.T) {
	raw, err := os.ReadFile("testdata/open-eyes-params-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Params OpenEyesParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	open := fixture.Params
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	sourceObservation, donorObservation := uuid.New(), uuid.New()
	proof := DonorConfirmation{SchemaVersion: 1, PersonID: uuid.New(),
		Source: open.Base, Donor: &open.Donor, SourceFaceID: open.SourceFaceID,
		DonorFaceID: &open.DonorFaceID, SourceObservationID: sourceObservation,
		DonorObservationID: &donorObservation, ConsentID: uuid.New(),
		Epoch: "1", ActorID: who.UserID}
	proofRaw, digest, err := proof.Compile()
	if err != nil {
		t.Fatal(err)
	}
	paramsRaw, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "mode": "donor", "base": open.Base,
		"donor": open.Donor, "strength": open.Strength,
		"glasses_mask": open.EyesMask, "protected": open.Protected,
		"confirmation": open.Confirmation, "source_faces": open.SourceFaces,
		"donor_faces": open.DonorFaces, "model_lock_sha": strings.Repeat("b", 64),
		"calibration": v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "wrong_role", "wrong_actor", "wrong_observation", "wrong_source", "unknown_field", "commit_failure"} {
		t.Run(mode, func(t *testing.T) {
			resultRaw := append([]byte(nil), proofRaw...)
			switch mode {
			case "wrong_actor", "wrong_observation", "wrong_source":
				changed := proof
				if mode == "wrong_actor" {
					changed.ActorID = uuid.New()
				} else if mode == "wrong_observation" {
					changed.DonorObservationID = new(uuid.UUID)
					*changed.DonorObservationID = uuid.New()
				} else {
					changed.Source.AssetVersionID = uuid.New()
				}
				resultRaw, _, err = changed.Compile()
				if err != nil {
					t.Fatal(err)
				}
			case "unknown_field":
				resultRaw = append([]byte(`{"untrusted":true,`), proofRaw[1:]...)
			}
			tx := &donorIssueTransaction{donorIdentityTx: donorIdentityTx{role: "photo_scheduler", raw: resultRaw}, failCommit: mode == "commit_failure"}
			if mode == "wrong_role" {
				tx.role = "photo_api"
			}
			db := &donorIssueDB{tx: tx}
			got, callErr := ReadGlareDonorPreparationProof(context.Background(), db, who,
				uuid.New(), uuid.New(), uuid.New(), uuid.New(), paramsRaw, []byte(`{}`),
				sourceObservation, donorObservation)
			expectedCalls := 2
			if mode == "wrong_role" {
				expectedCalls = 1
			}
			if db.begins != 1 || tx.execs != 1 || tx.calls != expectedCalls || tx.rollbacks != 1 {
				t.Fatal("preparation transaction did not enforce scheduler scope and cleanup")
			}
			if mode == "valid" {
				if callErr != nil || tx.commits != 1 || string(got.Proof) != string(proofRaw) ||
					got.Params.Confirmation.SHA256 != digest || got.Params.SourceFaceID != open.SourceFaceID ||
					got.Params.DonorFaceID != open.DonorFaceID {
					t.Fatal("valid preparation proof not bound", callErr)
				}
			} else if callErr == nil || len(got.Proof) != 0 || tx.commits != boolInt(mode == "commit_failure") {
				t.Fatal("untrusted preparation proof escaped", mode, callErr)
			}
		})
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestDeriveGlareDonorIssuerParamsFromBoundObservations(t *testing.T) {
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
	open := fixture.Params
	sourceObservation, donorObservation := uuid.New(), uuid.New()
	makeFaces := func(binding v2input.SessionVersionBinding, observation, face uuid.UUID) ([]byte, string) {
		points := make([]map[string]float64, 68)
		for i := range points {
			points[i] = map[string]float64{"x": .5, "y": .5}
		}
		body, e := canonical.Marshal(map[string]any{"schema_version": 1,
			"binding": binding, "faces": []any{map[string]any{
				"observation_id": observation, "face_id": face, "person_id": nil,
				"box":       map[string]float64{"x0": .25, "y0": .25, "x1": .75, "y1": .75},
				"landmarks": points, "embedding": nil, "quality": .9}}})
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(body)
		return body, hex.EncodeToString(sum[:])
	}
	sourceRaw, sourceDigest := makeFaces(open.Base, sourceObservation, open.SourceFaceID)
	donorRaw, donorDigest := makeFaces(open.Donor, donorObservation, open.DonorFaceID)
	open.SourceFaces.SHA256 = sourceDigest
	open.DonorFaces.SHA256 = donorDigest
	paramsRaw, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"mode": "donor", "base": open.Base, "donor": open.Donor,
		"strength": open.Strength, "glasses_mask": open.EyesMask,
		"protected": open.Protected, "confirmation": open.Confirmation,
		"source_faces": open.SourceFaces, "donor_faces": open.DonorFaces,
		"model_lock_sha": strings.Repeat("b", 64),
		"calibration":    v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DeriveGlareDonorIssuerParams(paramsRaw, sourceRaw, donorRaw,
		sourceObservation, donorObservation)
	if err != nil || got.SourceFaceID != open.SourceFaceID ||
		got.DonorFaceID != open.DonorFaceID || got.GlassesMask != open.EyesMask {
		t.Fatal("bound face derivation", err)
	}
	if _, err := DeriveGlareDonorIssuerParams(paramsRaw, donorRaw, sourceRaw,
		sourceObservation, donorObservation); err == nil {
		t.Fatal("substituted observation documents accepted")
	}
	if _, err := DeriveGlareDonorIssuerParams(paramsRaw, sourceRaw, donorRaw,
		uuid.New(), donorObservation); err == nil {
		t.Fatal("substituted observation identity accepted")
	}
	withUnknown := append([]byte(`{"unexpected":true,`), paramsRaw[1:]...)
	if _, err := DeriveGlareDonorIssuerParams(withUnknown, sourceRaw, donorRaw,
		sourceObservation, donorObservation); err == nil {
		t.Fatal("unknown Params field accepted before issuance")
	}
	issued := got
	issued.Confirmation.ArtifactID = uuid.New()
	issued.Confirmation.SHA256 = strings.Repeat("a", 64)
	final, err := BindGlareDonorConfirmation(paramsRaw, got, issued)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := DeriveGlareDonorIssuerParams(final, sourceRaw, donorRaw,
		sourceObservation, donorObservation)
	if err != nil || confirmed != issued {
		t.Fatal("confirmation was not bound to final Params", err)
	}
	unchangedID := issued
	unchangedID.Confirmation.ArtifactID = got.Confirmation.ArtifactID
	if _, err := BindGlareDonorConfirmation(paramsRaw, got, unchangedID); err == nil {
		t.Fatal("owner placeholder accepted as issued document ID")
	}
	changedMask := issued
	changedMask.GlassesMask = got.Protected
	if _, err := BindGlareDonorConfirmation(paramsRaw, got, changedMask); err == nil {
		t.Fatal("issued document changed frozen mask")
	}
	issued.Confirmation.ArtifactID = got.Base.Source.ArtifactID
	if _, err := BindGlareDonorConfirmation(paramsRaw, got, issued); err == nil {
		t.Fatal("colliding confirmation document accepted")
	}
	tx := &donorIssueTransaction{donorIdentityTx: donorIdentityTx{
		role: "photo_scheduler", raw: []byte(`{"source_face_id":null}`)}}
	db := &donorIssueDB{tx: tx}
	publisher := &donorIssuePublisher{tx: tx}
	if _, err := IssueGlareDonorFromObservations(context.Background(), db,
		publisher, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()},
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), paramsRaw,
		sourceObservation, donorObservation); err == nil || db.begins != 1 ||
		publisher.calls != 0 {
		t.Fatal("unqualified SQL face choice reached donor object publication")
	}
}
