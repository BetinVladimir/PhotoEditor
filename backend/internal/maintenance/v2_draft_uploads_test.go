package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type draftUploadRepo struct {
	ticket   *V2DraftUploadTicket
	finished bool
	retired  bool
}

func (r *draftUploadRepo) Claim(context.Context) (*V2DraftUploadTicket, error) { return r.ticket, nil }
func (r *draftUploadRepo) Finish(_ context.Context, _ V2DraftUploadTicket) (bool, error) {
	r.finished = true
	return true, nil
}
func (r *draftUploadRepo) Retire(context.Context) (bool, error) {
	r.retired = true
	return true, nil
}

type draftUploadObjects struct {
	err   error
	calls int
}

func (o *draftUploadObjects) PurgeV2Draft(context.Context, uuid.UUID, uuid.UUID) error {
	o.calls++
	return o.err
}

func TestV2DraftUploadCleanupFencesQuotaReleaseOnPurge(t *testing.T) {
	for _, scenario := range []string{"success", "purge-error", "wrong-bucket", "retire"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &draftUploadRepo{ticket: &V2DraftUploadTicket{
				TenantID: uuid.New(), DocumentID: uuid.New(), ArtifactID: uuid.New(),
				Token: uuid.New(), Bucket: "bucket"}}
			objects := &draftUploadObjects{}
			switch scenario {
			case "purge-error":
				objects.err = errors.New("storage unavailable")
			case "wrong-bucket":
				repo.ticket.Bucket = "other"
			case "retire":
				repo.ticket = nil
			}
			cleaned, err := (V2DraftUploadCleaner{Repository: repo,
				Objects: objects, Bucket: "bucket"}).CleanOne(context.Background())
			if scenario == "success" && (err != nil || !cleaned || !repo.finished || objects.calls != 1) {
				t.Fatalf("cleanup incomplete: %v", err)
			}
			if scenario == "retire" && (err != nil || !cleaned || !repo.retired || objects.calls != 0) {
				t.Fatalf("retire incomplete: %v", err)
			}
			if scenario == "purge-error" || scenario == "wrong-bucket" {
				if err == nil || cleaned || repo.finished {
					t.Fatalf("released quota without purge: %v", err)
				}
			}
		})
	}
}
