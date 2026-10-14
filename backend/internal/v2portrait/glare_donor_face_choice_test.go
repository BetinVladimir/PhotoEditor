package v2portrait

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

func TestGlareDonorFaceChoiceBindsScopedObservations(t *testing.T) {
	tenant, project, shoot, run := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sourceObservation, donorObservation := uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	params := []byte(`{"mode":"donor"}`)
	want := GlareDonorFaceChoice{SourceFaceID: uuid.New(), DonorFaceID: uuid.New()}
	raw, err := canonical.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	tx := &donorIssueTransaction{donorIdentityTx: donorIdentityTx{
		role: "photo_scheduler", raw: raw}}
	db := &donorIssueDB{tx: tx}
	got, err := ReadGlareDonorFaceChoice(context.Background(), db, who,
		tenant, project, shoot, run, params, sourceObservation, donorObservation)
	if err != nil || got != want || tx.commits != 1 || tx.tenantArg != tenant.String() ||
		len(tx.args) != 9 || tx.args[0] != tenant || tx.args[1] != project ||
		tx.args[2] != shoot || tx.args[3] != run || tx.args[4] != who.UserID ||
		tx.args[5] != who.SessionID || tx.args[6] != string(params) ||
		tx.args[7] != sourceObservation || tx.args[8] != donorObservation {
		t.Fatal("face choice lost run/actor/observation authority", err)
	}
	tx.role = "photo_api"
	tx.calls = 0
	if _, err := ReadGlareDonorFaceChoice(context.Background(), db, who,
		tenant, project, shoot, run, params, sourceObservation, donorObservation); err == nil || tx.calls != 1 {
		t.Fatal("non-scheduler reached face choice")
	}
	tx.role = "photo_scheduler"
	tx.raw = []byte(`{"source_face_id":null,"donor_face_id":null}`)
	if _, err := ReadGlareDonorFaceChoice(context.Background(), db, who,
		tenant, project, shoot, run, params, sourceObservation, donorObservation); err == nil {
		t.Fatal("empty face choice accepted")
	}
}
