package workflow

import (
	"context"
	"errors"
	"photoeditor/backend/internal/artifactjson"
	"strings"
	"testing"
)

func TestPerspectiveLinesActivationRetainsSourceWitnessAndSQLRefusal(t *testing.T) {
	f := newLinesInputsFixture(t, true)
	verified, err := f.verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	witness, err := verified.AdmissionJSON(f.plan, "photos")
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("database unavailable")
	for _, test := range []struct {
		accepted       bool
		queryErr, want error
	}{{true, nil, nil}, {false, nil, artifactjson.ErrStale}, {false, failure, failure}} {
		tx := &linesSealReceiptTx{accepted: test.accepted, err: test.queryErr}
		err := activatePerspectiveLinesInTx(context.Background(), tx, f.plan.scope.Tenant, f.plan.scope.Run, witness)
		if !errors.Is(err, test.want) {
			t.Fatal(err, test.want)
		}
		if !strings.Contains(tx.query, "photo.activate_v29_perspective_lines") || len(tx.args) != 3 || tx.args[0] != f.plan.scope.Tenant || tx.args[1] != f.plan.scope.Run || tx.args[2] != string(witness) {
			t.Fatal("lost activation scope/witness")
		}
	}
}
func TestPerspectiveLinesActivationRequiresVerificationAndDatabase(t *testing.T) {
	f := newLinesInputsFixture(t, false)
	if err := ActivatePerspectiveLines(context.Background(), nil, f.plan, VerifiedPerspectiveLinesInputs{}, "photos"); err == nil {
		t.Fatal("missing verification accepted")
	}
	verified, err := f.verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := ActivatePerspectiveLines(context.Background(), nil, f.plan, verified, "photos"); err == nil {
		t.Fatal("missing database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ActivatePerspectiveLines(ctx, nil, f.plan, verified, "photos"); err != context.Canceled {
		t.Fatal(err)
	}
}
