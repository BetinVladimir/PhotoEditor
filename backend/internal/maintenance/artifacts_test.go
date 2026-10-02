package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

type artifactGCPorts struct {
	ticket            *ArtifactGC
	recovered         int
	removeErr         error
	removed, finished bool
}

func (p *artifactGCPorts) Recover(context.Context) (int, error)       { return p.recovered, nil }
func (p *artifactGCPorts) Claim(context.Context) (*ArtifactGC, error) { return p.ticket, nil }
func (p *artifactGCPorts) Finish(context.Context, ArtifactGC) (bool, error) {
	p.finished = true
	return true, nil
}
func (p *artifactGCPorts) RemoveConfirmed(context.Context, objectstore.Object) error {
	p.removed = true
	return p.removeErr
}
func TestArtifactGCFinishesOnlyConfirmedExactVersion(t *testing.T) {
	fixture := func() *ArtifactGC {
		return &ArtifactGC{ArtifactID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), Bucket: "bucket", Object: objectstore.Object{Key: "tenants/t/object", VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 10, ContentType: "image/jpeg"}}
	}
	for _, mode := range []string{"success", "remove-failed", "unversioned", "foreign-bucket", "idle-recovered"} {
		t.Run(mode, func(t *testing.T) {
			p := &artifactGCPorts{ticket: fixture()}
			switch mode {
			case "remove-failed":
				p.removeErr = errors.New("storage unavailable")
			case "unversioned":
				p.ticket.Object.VersionID = "null"
			case "foreign-bucket":
				p.ticket.Bucket = "other"
			case "idle-recovered":
				p.ticket = nil
				p.recovered = 2
			}
			done, err := (ArtifactCleaner{p, p, "bucket"}).CleanOne(context.Background())
			if mode == "success" {
				if err != nil || !done || !p.removed || !p.finished {
					t.Fatal(done, err)
				}
				return
			}
			if mode == "idle-recovered" {
				if err != nil || !done || p.removed || p.finished {
					t.Fatal(done, err)
				}
				return
			}
			if err == nil || done || p.finished {
				t.Fatal("unsafe GC completion", done, err)
			}
		})
	}
}
