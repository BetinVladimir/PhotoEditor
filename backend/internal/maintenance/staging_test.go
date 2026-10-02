package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

type stagingPorts struct {
	ticket            *StagingCleanup
	removeErr         error
	removed, finished bool
	seen              objectstore.Object
}

func (p *stagingPorts) Claim(context.Context) (*StagingCleanup, error) { return p.ticket, nil }
func (p *stagingPorts) RemoveConfirmed(_ context.Context, o objectstore.Object) error {
	p.removed = true
	p.seen = o
	return p.removeErr
}
func (p *stagingPorts) Finish(context.Context, StagingCleanup) (bool, error) {
	p.finished = true
	return true, nil
}
func TestStagingCleanerRequiresConfirmedVersionDeletion(t *testing.T) {
	for _, mode := range []string{"success", "delete unknown", "wrong bucket", "unversioned", "idle"} {
		t.Run(mode, func(t *testing.T) {
			p := &stagingPorts{ticket: &StagingCleanup{ID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), ArtifactID: uuid.New(), Bucket: "bucket", Object: objectstore.Object{Key: "staging/source", VersionID: "v1", SHA256: strings.Repeat("a", 64), ContentType: "image/jpeg", Bytes: 1024}}}
			switch mode {
			case "delete unknown":
				p.removeErr = errors.New("HEAD unavailable")
			case "wrong bucket":
				p.ticket.Bucket = "other"
			case "unversioned":
				p.ticket.Object.VersionID = "null"
			case "idle":
				p.ticket = nil
			}
			done, err := (StagingCleaner{p, p, "bucket"}).CleanOne(context.Background())
			if mode == "success" {
				if err != nil || !done || !p.finished || p.seen != p.ticket.Object {
					t.Fatal("version not preserved", err)
				}
				return
			}
			if done || p.finished {
				t.Fatal("quota released without proof")
			}
			if mode != "delete unknown" && p.removed {
				t.Fatal("invalid ticket removed object")
			}
			if mode != "idle" && err == nil {
				t.Fatal("cleanup error hidden")
			}
		})
	}
}
