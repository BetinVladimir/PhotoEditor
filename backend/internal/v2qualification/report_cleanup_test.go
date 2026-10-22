package v2qualification

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type reportCleanupUnitRepo struct {
	ticket           ReportCleanupTicket
	closed, finished int
	denyClose        bool
}

func (r *reportCleanupUnitRepo) Claim(context.Context) (*ReportCleanupTicket, error) {
	copy := r.ticket
	return &copy, nil
}
func (r *reportCleanupUnitRepo) CloseWriter(context.Context, ReportCleanupTicket, string) error {
	r.closed++
	if r.denyClose {
		return ErrUnqualified
	}
	return nil
}
func (r *reportCleanupUnitRepo) Finish(context.Context, ReportCleanupTicket) error {
	r.finished++
	return nil
}

type reportCleanupUnitObjects struct {
	object                 objectstore.Object
	missing, deleteFailure bool
	recovered, removed     int
}

func (o *reportCleanupUnitObjects) RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error) {
	o.recovered++
	if o.missing {
		return objectstore.Object{}, objectstore.ErrMissing
	}
	return o.object, nil
}
func (o *reportCleanupUnitObjects) RemoveConfirmed(context.Context, objectstore.Object) error {
	o.removed++
	if o.deleteFailure {
		return objectstore.ErrUnavailable
	}
	return nil
}
func TestReportCleanupNeverRefundsUnknownOpenWriter(t *testing.T) {
	for _, mode := range []string{"idle", "open-missing", "open-found", "closed", "delete-failure", "lost-close-token"} {
		t.Run(mode, func(t *testing.T) {
			tenant, project, id := uuid.New(), uuid.New(), uuid.New()
			scope := ReportPublicationScope{Tenant: tenant, Project: project}
			version := "original-version"
			row := ReportCleanupTicket{Artifact: id, Tenant: tenant, Project: project, Token: uuid.New(), WriterState: "open", Key: reportPublicationKey(scope, id), SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bytes: 10, Bucket: "photos"}
			repo := &reportCleanupUnitRepo{ticket: row}
			objects := &reportCleanupUnitObjects{object: objectstore.Object{Key: row.Key, VersionID: version, SHA256: row.SHA, Bytes: row.Bytes, ContentType: "application/json"}}
			switch mode {
			case "idle":
				repo.ticket.WriterState = "idle"
			case "open-missing":
				objects.missing = true
			case "closed":
				repo.ticket.WriterState = "closed"
				repo.ticket.Version = &version
			case "delete-failure":
				objects.deleteFailure = true
			case "lost-close-token":
				repo.denyClose = true
			}
			progress, err := (ReportPublicationCleanup{Repository: repo, Objects: objects, Bucket: "photos"}).ProcessOne(context.Background())
			positive := mode == "idle" || mode == "open-found" || mode == "closed"
			if positive {
				if !progress || err != nil || repo.finished != 1 {
					t.Fatal("valid cleanup did not settle", err)
				}
			} else {
				if progress || err == nil || repo.finished != 0 {
					t.Fatal("unsafe cleanup released quota")
				}
			}
			if mode == "idle" && (objects.recovered != 0 || objects.removed != 0) {
				t.Fatal("idle producer unexpectedly touched objects")
			}
			if mode == "open-missing" && (repo.closed != 0 || objects.removed != 0) {
				t.Fatal("absent open writer declared dead")
			}
			if mode == "lost-close-token" && objects.removed != 0 {
				t.Fatal("lost cleanup authority deleted")
			}
		})
	}
}
