package objectstore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

func TestGenerationDiscoveryIsOnlyScopedObservation(t *testing.T) {
	key, _ := InterchangeGenerationKey(uuid.New(), uuid.New(), uuid.New())
	for _, mode := range []string{"empty", "foreign", "one", "duplicate", "invalid", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			called := false
			if mode == "cancelled" {
				cancel()
			}
			id, err := discoverExactMultipart(ctx, key, func(context.Context) <-chan minio.ObjectMultipartInfo {
				called = true
				rows := make(chan minio.ObjectMultipartInfo, 2)
				switch mode {
				case "foreign":
					rows <- minio.ObjectMultipartInfo{Key: key + "-other", UploadID: "foreign"}
				case "one":
					rows <- minio.ObjectMultipartInfo{Key: key, UploadID: "observed"}
				case "duplicate":
					rows <- minio.ObjectMultipartInfo{Key: key, UploadID: "first"}
					rows <- minio.ObjectMultipartInfo{Key: key, UploadID: "second"}
				case "invalid":
					rows <- minio.ObjectMultipartInfo{Key: key, UploadID: "bad\x00id"}
				}
				close(rows)
				return rows
			})
			switch mode {
			case "empty", "foreign":
				if err != nil || id != "" {
					t.Fatal(id, err)
				}
			case "one":
				if err != nil || id != "observed" {
					t.Fatal(id, err)
				}
			default:
				if err == nil || id != "" {
					t.Fatal("ambiguous discovery accepted", id, err)
				}
			}
			if mode == "cancelled" && called {
				t.Fatal("cancelled discovery reached storage")
			}
		})
	}
}

func TestGenerationDrainRequiresKnownIDAndPositiveAbsence(t *testing.T) {
	key, _ := InterchangeGenerationKey(uuid.New(), uuid.New(), uuid.New())
	for _, mode := range []string{"ok", "absent", "live", "listed", "abort-error", "cancelled", "empty-id"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			id := "session"
			if mode == "empty-id" {
				id = ""
			}
			calls := 0
			err := drainExactGenerationMultipart(ctx, key, id,
				func(context.Context) error {
					calls++
					if mode == "absent" {
						return minio.ErrorResponse{Code: "NoSuchUpload"}
					}
					if mode == "abort-error" {
						return errors.New("transport")
					}
					return nil
				},
				func(context.Context) error {
					calls++
					if mode == "live" {
						return nil
					}
					return minio.ErrorResponse{Code: "NoSuchUpload"}
				},
				func(context.Context) <-chan minio.ObjectMultipartInfo {
					calls++
					rows := make(chan minio.ObjectMultipartInfo, 1)
					if mode == "listed" {
						rows <- minio.ObjectMultipartInfo{Key: key, UploadID: id}
					}
					close(rows)
					return rows
				})
			if (err == nil) != (mode == "ok" || mode == "absent") {
				t.Fatal(mode, err)
			}
			if (mode == "cancelled" || mode == "empty-id") && calls != 0 {
				t.Fatal("unsafe drain IO", mode, calls)
			}
		})
	}
}

func TestGenerationPurgeRequiresExactKeyAndCompletedListing(t *testing.T) {
	key, _ := InterchangeGenerationKey(uuid.New(), uuid.New(), uuid.New())
	for _, mode := range []string{"empty", "listed", "purge-error", "list-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			purges, lists := 0, 0
			err := purgeExactInterchangeKey(ctx, key, func(context.Context, string) error {
				purges++
				if mode == "purge-error" {
					return ErrUnavailable
				}
				return nil
			}, func(context.Context, string) <-chan minio.ObjectMultipartInfo {
				lists++
				rows := make(chan minio.ObjectMultipartInfo, 1)
				if mode == "listed" {
					rows <- minio.ObjectMultipartInfo{Key: key, UploadID: "still-live"}
				}
				if mode == "list-error" {
					rows <- minio.ObjectMultipartInfo{Err: errors.New("listing failed")}
				}
				close(rows)
				return rows
			})
			if (err == nil) != (mode == "empty") {
				t.Fatal(mode, err)
			}
			if mode == "cancelled" && (purges != 0 || lists != 0) {
				t.Fatal("cancelled generation purge touched storage")
			}
			if mode == "purge-error" && lists != 0 {
				t.Fatal("listed after failed purge")
			}
		})
	}
	var store *Store
	if err := store.PurgeInterchangeGeneration(context.Background(), uuid.Nil, uuid.New(), uuid.New()); err != ErrInvalid {
		t.Fatal("invalid generation reached storage", err)
	}
	tenant, upload, generation := uuid.New(), uuid.New(), uuid.New()
	key, _ = InterchangeGenerationKey(tenant, upload, generation)
	if err := store.PurgeInterchangeArchiveKey(context.Background(), uuid.New(), upload, key); err != ErrInvalid {
		t.Fatal("foreign tenant purge reached storage", err)
	}
	if err := store.PurgeInterchangeArchiveKey(context.Background(), tenant, uuid.New(), key); err != ErrInvalid {
		t.Fatal("foreign upload purge reached storage", err)
	}
}
