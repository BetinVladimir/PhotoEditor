package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"testing"
)

type noPerspectivePreparationIO struct{}

func (noPerspectivePreparationIO) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	panic("unexpected object read")
}
func (noPerspectivePreparationIO) PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error) {
	panic("unexpected object write")
}
func (noPerspectivePreparationIO) RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error) {
	panic("unexpected object recovery")
}

type noPerspectivePreparationDB struct{}

func (noPerspectivePreparationDB) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected database IO")
}

func TestPerspectiveCoordinatorMalformedPreparationDoesNotTouchIO(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	p := perspectiveIntent()
	p.Base.Source.SHA256, err = canonical.Hash(image)
	if err != nil {
		t.Fatal(err)
	}
	p.Base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	required, err := PerspectiveResources(image.Canvas.Width, image.Canvas.Height, image.Canvas.Width, image.Canvas.Height)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"params", "source", "tenant", "project", "snapshot", "actor", "session", "key", "max_pixels", "capacity", "reader", "writer", "db", "bucket", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			c := PerspectiveCoordinator{DB: noPerspectivePreparationDB{}, Reader: noPerspectivePreparationIO{}, Writer: noPerspectivePreparationIO{}, Bucket: "photos", MaxPixels: 200000000, Capacity: required}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			request := PerspectivePrepareRequest{Tenant: uuid.New(), Project: uuid.New(), Snapshot: uuid.New(), Key: "prepare-perspective-test", Params: mustPerspectiveParams(t, p), Source: append([]byte(nil), raw...)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "params":
				request.Params = []byte("{}")
			case "source":
				request.Source = []byte("{}")
			case "tenant":
				request.Tenant = uuid.Nil
			case "project":
				request.Project = uuid.Nil
			case "snapshot":
				request.Snapshot = uuid.Nil
			case "actor":
				who.UserID = uuid.Nil
			case "session":
				who.SessionID = uuid.Nil
			case "key":
				request.Key = ""
			case "max_pixels":
				c.MaxPixels = 0
			case "capacity":
				c.Capacity.RAMBytes = 0
			case "reader":
				c.Reader = nil
			case "writer":
				c.Writer = nil
			case "db":
				c.DB = nil
			case "bucket":
				c.Bucket = ""
			case "cancel":
				cancel()
			}
			if fault != "writer" {
				if _, err := c.ReadQueuedReceipt(ctx, who, request); err == nil {
					t.Fatal("accepted malformed queued receipt request", fault)
				}
				if _, err := c.RecoverPrepared(ctx, who, request); err == nil {
					t.Fatal("accepted malformed recovery", fault)
				}
			}
			if _, err := c.PrepareParams(ctx, who, request); err == nil {
				t.Fatal("accepted malformed preparation", fault)
			}
		})
	}
}
