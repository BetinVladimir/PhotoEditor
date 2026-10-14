package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestGlareDonorIntentRejectsAuthorityAndUnknownFields(t *testing.T) {
	intent := GlareDonorClientIntent{SchemaVersion: 1, Shoot: uuid.New(),
		Snapshot: uuid.New(), Ordinal: 0, Proposal: []byte(`{"schema_version":1}`),
		SourceObservation: uuid.New(), DonorObservation: uuid.New()}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeGlareDonorClientIntent(raw)
	if err != nil || got.Shoot != intent.Shoot || got.SourceObservation != intent.SourceObservation {
		t.Fatal("canonical authored selection rejected", err)
	}
	for _, bad := range [][]byte{
		append([]byte(` `), raw...),
		[]byte(`{"schema_version":1,"qualification":{},"shoot_session_id":"` + intent.Shoot.String() + `"}`),
	} {
		if _, err := DecodeGlareDonorClientIntent(bad); err != v2input.ErrInvalid {
			t.Fatal("client authority or noncanonical JSON accepted", err)
		}
	}
	var service *GlareDonorSubmissionService
	if _, err := service.SubmitProjectKeyed(context.Background(), auth.Identity{
		UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(),
		"key", got); err != v2input.ErrInvalid {
		t.Fatal("uninstalled glare donor service accepted request", err)
	}
}
