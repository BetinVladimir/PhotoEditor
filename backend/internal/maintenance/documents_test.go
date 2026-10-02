package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type documentRepo struct {
	ticket   *DocumentCleanup
	finished bool
}

func (r *documentRepo) Claim(context.Context) (*DocumentCleanup, error) { return r.ticket, nil }
func (r *documentRepo) Finish(_ context.Context, t DocumentCleanup) (bool, error) {
	r.finished = true
	return true, nil
}

type documentObjects struct {
	err                   error
	calls                 int
	tenant, run, artifact uuid.UUID
}

func (o *documentObjects) PurgeDocument(_ context.Context, t, r, a uuid.UUID) error {
	o.calls++
	o.tenant = t
	o.run = r
	o.artifact = a
	return o.err
}
func TestDocumentCleanupOnlyFinishesSuccessfulPurge(t *testing.T) {
	for _, scenario := range []string{"success", "purge-failed", "foreign-bucket", "invalid-id", "no-ticket"} {
		t.Run(scenario, func(t *testing.T) {
			ticket := &DocumentCleanup{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "test-bucket"}
			repo := &documentRepo{ticket: ticket}
			objects := &documentObjects{}
			switch scenario {
			case "purge-failed":
				objects.err = errors.New("purge failed")
			case "foreign-bucket":
				ticket.Bucket = "other-bucket"
			case "invalid-id":
				ticket.Token = uuid.Nil
			case "no-ticket":
				repo.ticket = nil
			}
			done, err := (DocumentCleaner{repo, objects, "test-bucket"}).CleanOne(context.Background())
			if scenario == "success" {
				if err != nil || !done || !repo.finished || objects.artifact != ticket.ArtifactID || objects.run != ticket.RunID || objects.tenant != ticket.TenantID {
					t.Fatal("cleanup incomplete", err)
				}
			} else {
				if done || repo.finished {
					t.Fatal("released quota before purge")
				}
				if scenario == "no-ticket" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil {
					t.Fatal("missing failure")
				}
				if scenario != "purge-failed" && objects.calls != 0 {
					t.Fatal("unauthorized purge")
				}
			}
		})
	}
}
