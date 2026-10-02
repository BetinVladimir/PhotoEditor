package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type originalCleanupPorts struct {
	ticket           *OriginalCleanup
	purgeErr         error
	purged, finished bool
}

func (p *originalCleanupPorts) Claim(context.Context) (*OriginalCleanup, error) { return p.ticket, nil }
func (p *originalCleanupPorts) Finish(context.Context, OriginalCleanup) (bool, error) {
	p.finished = true
	return true, nil
}
func (p *originalCleanupPorts) PurgeOriginal(_ context.Context, tenant, artifact uuid.UUID) error {
	if tenant != p.ticket.TenantID || artifact != p.ticket.ArtifactID {
		return errors.New("wrong key identity")
	}
	p.purged = true
	return p.purgeErr
}
func TestOriginalCleanupOnlyReleasesAfterPurge(t *testing.T) {
	for _, mode := range []string{"success", "purge failed", "foreign bucket", "empty"} {
		t.Run(mode, func(t *testing.T) {
			p := &originalCleanupPorts{ticket: &OriginalCleanup{uuid.New(), uuid.New(), uuid.New(), "bucket"}}
			switch mode {
			case "purge failed":
				p.purgeErr = errors.New("storage unavailable")
			case "foreign bucket":
				p.ticket.Bucket = "foreign"
			case "empty":
				p.ticket = nil
			}
			done, err := (OriginalCleaner{p, p, "bucket"}).CleanOne(context.Background())
			if mode == "success" {
				if err != nil || !done || !p.purged || !p.finished {
					t.Fatal("cleanup failed", err)
				}
				return
			}
			if done || p.finished {
				t.Fatal("quota released before successful purge")
			}
			if mode == "foreign bucket" && p.purged {
				t.Fatal("foreign bucket purged")
			}
			if mode != "empty" && err == nil {
				t.Fatal("error hidden")
			}
		})
	}
}
