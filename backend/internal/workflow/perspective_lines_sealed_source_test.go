package workflow

import (
	"context"
	"errors"
	"photoeditor/backend/internal/artifactjson"
	"strings"
	"testing"
)

func TestPerspectiveLinesSealedSourcePreservesFreshWitnessAndRefusal(t *testing.T) {
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
		err := perspectiveLinesSealedSourceInTx(context.Background(), tx, f.plan, verified, "photos")
		if !errors.Is(err, test.want) {
			t.Fatal(err, test.want)
		}
		if !strings.Contains(tx.query, "photo.v29_perspective_lines_sealed_source_ready") || len(tx.args) != 3 || tx.args[0] != f.plan.scope.Tenant || tx.args[1] != f.plan.scope.Run || tx.args[2] != string(witness) {
			t.Fatal("lost scope or witness")
		}
	}
}
func TestPerspectiveLinesSealedSourceRejectsMissingWitnessBeforeQuery(t *testing.T) {
	f := newLinesInputsFixture(t, false)
	tx := &linesSealReceiptTx{accepted: true}
	if err := perspectiveLinesSealedSourceInTx(context.Background(), tx, f.plan, VerifiedPerspectiveLinesInputs{}, "photos"); err == nil {
		t.Fatal("unverified source accepted")
	}
	if tx.query != "" {
		t.Fatal("query before preflight")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := perspectiveLinesSealedSourceInTx(ctx, tx, f.plan, VerifiedPerspectiveLinesInputs{}, "photos"); err != context.Canceled {
		t.Fatal(err)
	}
}
