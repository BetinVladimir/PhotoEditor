package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type originalPorts struct {
	reservation OriginalReservation
	source      objectstore.Object
	calls       []string
	mode        string
}

func (p *originalPorts) Reserve(context.Context, OriginalScope) (OriginalReservation, error) {
	p.calls = append(p.calls, "reserve")
	return p.reservation, nil
}
func (p *originalPorts) Source(context.Context, OriginalScope, OriginalReservation) (objectstore.Object, error) {
	p.calls = append(p.calls, "source")
	return p.source, nil
}
func (p *originalPorts) CopyOriginal(_ context.Context, tenant, id uuid.UUID, source objectstore.Object) (objectstore.Object, error) {
	p.calls = append(p.calls, "copy")
	if p.mode == "copy error" {
		return objectstore.Object{}, errors.New("copy failed")
	}
	target := source
	target.Key, _ = objectstore.OriginalKey(tenant, id)
	target.VersionID = "original-v1"
	if p.mode == "wrong copy" {
		target.SHA256 = "bad"
	}
	return target, nil
}
func (p *originalPorts) Complete(_ context.Context, _ OriginalScope, res OriginalReservation, v string) (bool, error) {
	p.calls = append(p.calls, "complete")
	if res.Token != p.reservation.Token || v != "original-v1" {
		return false, errors.New("wrong fence/version")
	}
	if p.mode == "lost fence" {
		return false, nil
	}
	if p.mode == "uncertain commit" {
		p.reservation.Ready = true
		p.reservation.Version = v
		return false, errors.New("response lost")
	}
	return true, nil
}
func originalCopyFixture(t *testing.T) (OriginalScope, *originalPorts) {
	spec, _ := ingestFixture(t)
	s := OriginalScope{uuid.New(), spec.RunID, spec.Items[0].Ordinal, spec.Items[0].Source}
	p := &originalPorts{reservation: OriginalReservation{ArtifactID: uuid.New(), Token: uuid.New()}, source: objectstore.Object{Key: "staging/source", VersionID: "staging-v1", SHA256: s.Source.SHA256, Bytes: 1024, ContentType: "image/jpeg"}}
	return s, p
}
func TestOriginalCoordinatorOrderAndUncertainCommitResume(t *testing.T) {
	s, p := originalCopyFixture(t)
	p.mode = "uncertain commit"
	c := OriginalCoordinator{p, p, p}
	if _, err := c.Prepare(context.Background(), s); err == nil {
		t.Fatal("uncertain completion reported successful")
	}
	if len(p.calls) != 4 || p.calls[0] != "reserve" || p.calls[1] != "source" || p.calls[2] != "copy" || p.calls[3] != "complete" {
		t.Fatal("wrong transaction/IO order")
	}
	p.calls = nil
	ref, err := c.Prepare(context.Background(), s)
	if err != nil || ref.ID != p.reservation.ArtifactID || ref.SHA256 != s.Source.SHA256 || len(p.calls) != 1 || p.calls[0] != "reserve" {
		t.Fatal("ready replay recopied original", err)
	}
}
func TestOriginalCoordinatorNeverCompletesBadCopy(t *testing.T) {
	for _, mode := range []string{"copy error", "wrong copy", "lost fence", "success"} {
		t.Run(mode, func(t *testing.T) {
			s, p := originalCopyFixture(t)
			p.mode = mode
			c := OriginalCoordinator{p, p, p}
			ref, err := c.Prepare(context.Background(), s)
			if mode == "success" {
				if err != nil || ref.ID != p.reservation.ArtifactID {
					t.Fatal(err)
				}
				return
			}
			if err == nil || ref.ID != uuid.Nil {
				t.Fatal("invalid original exposed")
			}
			if (mode == "copy error" || mode == "wrong copy") && len(p.calls) != 3 {
				t.Fatal("bad copy reached completion")
			}
		})
	}
}
func TestOriginalSourceReaderPinsScopeAndBucket(t *testing.T) {
	s, p := originalCopyFixture(t)
	db := &expansionQueries{row: expansionRow{values: []any{"bucket", p.source.Key, p.source.VersionID, p.source.SHA256, p.source.Bytes, p.source.ContentType}}}
	repo := OriginalRepository{DB: db, Bucket: "bucket"}
	got, err := repo.Source(context.Background(), s, p.reservation)
	if err != nil || got != p.source || len(db.args) != 5 || db.args[4] != p.reservation.Token || db.args[2] != s.Ordinal {
		t.Fatal("source scope lost", err)
	}
	repo.Bucket = "other"
	if _, err = repo.Source(context.Background(), s, p.reservation); err == nil {
		t.Fatal("foreign bucket accepted")
	}
}
