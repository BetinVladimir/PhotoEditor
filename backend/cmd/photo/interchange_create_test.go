package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

func TestInterchangeCreateUnconfiguredKeepsStartupAvailable(t *testing.T) {
	t.Setenv("V30_INTERCHANGE_SCHEDULER_DATABASE_URL", "")
	service, close, err := buildInterchangeCreate(context.Background(), nil, nil, "")
	if err != nil || service != nil || close == nil {
		t.Fatal("unconfigured interchange changed startup", err)
	}
	close()
}
func TestInterchangeCreateConfiguredRefusesMissingPortsWithoutLeakingDSN(t *testing.T) {
	t.Setenv("V30_INTERCHANGE_SCHEDULER_DATABASE_URL", "postgres://owner:secret@invalid/interchange")
	service, close, err := buildInterchangeCreate(context.Background(), nil, nil, "documents")
	if err == nil || service != nil || close == nil {
		t.Fatal("missing dependencies accepted")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "postgres://") {
		t.Fatal("credential leaked")
	}
	close()
}

func TestInterchangeCreateCancellationBeforeConfiguration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service, close, err := buildInterchangeCreate(ctx, nil, nil, "")
	if err != context.Canceled || service != nil || close == nil {
		t.Fatal("cancelled startup continued", err)
	}
	close()
}

func TestInterchangeCreateRefusesInvalidExportInjection(t *testing.T) {
	t.Setenv("V30_INTERCHANGE_SCHEDULER_DATABASE_URL", "")
	for _, ports := range [][]interchange.ExportCreatePreparer{{nil}, {nil, nil}} {
		service, close, err := buildInterchangeCreate(context.Background(), nil, nil, "photos", ports...)
		if err == nil || service != nil || close == nil {
			t.Fatal("invalid export injection accepted")
		}
		close()
	}
}

// Embedded port panics if installation attempts request-level preparation.
type installedInterchangeExport struct {
	interchange.ExportCreatePreparer
}

func TestInterchangeExportFactoryOwnershipAndRefusal(t *testing.T) {
	for _, mode := range []string{"valid", "failure", "partial_failure", "nil", "cancel_before", "cancel_during", "same_pool"} {
		t.Run(mode, func(t *testing.T) {
			api, scheduler := new(pgxpool.Pool), new(pgxpool.Pool)
			if mode == "same_pool" {
				scheduler = api
			}
			objects := new(objectstore.Store)
			preparer := &installedInterchangeExport{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel_before" {
				cancel()
			}
			calls, closed := 0, 0
			factory := func(got context.Context, a, s *pgxpool.Pool, o *objectstore.Store, b string) (interchange.ExportCreatePreparer, func(), error) {
				calls++
				if got != ctx || a != api || s != scheduler || o != objects || b != "photos" {
					t.Fatal("factory received different process dependencies")
				}
				cleanup := func() { closed++ }
				if mode == "failure" {
					return nil, cleanup, errors.New("postgres://secret")
				}
				if mode == "partial_failure" {
					return preparer, cleanup, errors.New("secret")
				}
				if mode == "nil" {
					return nil, cleanup, nil
				}
				if mode == "cancel_during" {
					cancel()
				}
				return preparer, cleanup, nil
			}
			got, close, err := installInterchangeExportFactory(ctx, api, scheduler, objects, "photos", factory)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret") || got != nil) {
				t.Fatal("partial installation or credentials leaked")
			}
			if close == nil {
				t.Fatal("missing cleanup")
			}
			if mode == "valid" {
				if got != preparer || closed != 0 {
					t.Fatal("installation not retained")
				}
				close()
				close()
				if closed != 1 {
					t.Fatal("cleanup not idempotent")
				}
			} else {
				expected := 1
				if mode == "cancel_before" || mode == "same_pool" {
					expected = 0
				}
				if closed != expected || calls != expected {
					t.Fatalf("calls=%d closed=%d want=%d", calls, closed, expected)
				}
				close()
				if closed != expected {
					t.Fatal("cleanup repeated after failure")
				}
			}
		})
	}
}
func TestInterchangeExportFactoryRequiresSchedulerDeclaration(t *testing.T) {
	t.Setenv("V30_INTERCHANGE_SCHEDULER_DATABASE_URL", "")
	factory := func(context.Context, *pgxpool.Pool, *pgxpool.Pool, *objectstore.Store, string) (interchange.ExportCreatePreparer, func(), error) {
		t.Fatal("unconfigured factory invoked")
		return nil, nil, nil
	}
	service, close, err := buildInterchangeCreateWithFactory(context.Background(), nil, nil, "photos", factory)
	if err == nil || service != nil || close == nil {
		t.Fatal("missing scheduler accepted")
	}
	close()
	service, close, err = buildInterchangeCreateWithFactory(context.Background(), nil, nil, "photos", factory, &installedInterchangeExport{})
	if err == nil || service != nil || close == nil {
		t.Fatal("competing export declarations accepted")
	}
	close()
}
