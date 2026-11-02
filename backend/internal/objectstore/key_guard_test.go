package objectstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

// ── nil-UUID guard paths for key constructors ─────────────────────────────────

func TestOriginalKeyRejectsNilUUIDs(t *testing.T) {
	if _, err := OriginalKey(uuid.Nil, uuid.New()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil tenant must return ErrInvalid, got %v", err)
	}
	if _, err := OriginalKey(uuid.New(), uuid.Nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil artifact must return ErrInvalid, got %v", err)
	}
}

func TestDocumentKeyRejectsNilUUIDs(t *testing.T) {
	r, a := uuid.New(), uuid.New()
	if _, err := DocumentKey(uuid.Nil, r, a); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil tenant must return ErrInvalid, got %v", err)
	}
	if _, err := DocumentKey(uuid.New(), uuid.Nil, a); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil run must return ErrInvalid, got %v", err)
	}
	if _, err := DocumentKey(uuid.New(), r, uuid.Nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil artifact must return ErrInvalid, got %v", err)
	}
}

func TestV2DraftKeyRejectsNilUUIDs(t *testing.T) {
	if _, err := V2DraftKey(uuid.Nil, uuid.New()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil tenant must return ErrInvalid, got %v", err)
	}
	if _, err := V2DraftKey(uuid.New(), uuid.Nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil artifact must return ErrInvalid, got %v", err)
	}
}

func TestAttemptKeyRejectsNilUUIDsAndBadAttempt(t *testing.T) {
	tid, task, art := uuid.New(), uuid.New(), uuid.New()
	if _, err := AttemptKey(uuid.Nil, task, art, 1, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil tenant must return ErrInvalid, got %v", err)
	}
	if _, err := AttemptKey(tid, uuid.Nil, art, 1, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil task must return ErrInvalid, got %v", err)
	}
	if _, err := AttemptKey(tid, task, uuid.Nil, 1, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil artifact must return ErrInvalid, got %v", err)
	}
	if _, err := AttemptKey(tid, task, art, 0, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("attempt=0 must return ErrInvalid, got %v", err)
	}
	if _, err := AttemptKey(tid, task, art, 7, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("attempt=7 must return ErrInvalid, got %v", err)
	}
	if _, err := AttemptKey(tid, task, art, 1, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fence=0 must return ErrInvalid, got %v", err)
	}
}

// ── validInterchangeArchiveKey covers both upload-key and generation-key paths ─

func TestValidInterchangeArchiveKeyAcceptsGenerationKey(t *testing.T) {
	gen, _ := InterchangeGenerationKey(uuid.New(), uuid.New(), uuid.New())
	if !validInterchangeArchiveKey(gen) {
		t.Fatal("valid generation key must be accepted by validInterchangeArchiveKey")
	}
}

// ── inner pure-function guard paths ──────────────────────────────────────────

func TestDiscoverInterchangeMultipartRejectsInvalidKey(t *testing.T) {
	_, err := discoverInterchangeMultipart(context.Background(), "not-a-valid-interchange-key", func(context.Context) <-chan minio.ObjectMultipartInfo {
		ch := make(chan minio.ObjectMultipartInfo)
		close(ch)
		return ch
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid key must return ErrInvalid, got %v", err)
	}
}

func TestDiscoverInterchangeMultipartRejectsNilList(t *testing.T) {
	key, _ := InterchangeUploadKey(uuid.New(), uuid.New())
	_, err := discoverInterchangeMultipart(context.Background(), key, nil)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil list must return ErrInvalid, got %v", err)
	}
}

func TestAwaitInterchangeMultipartAbsentRejectsInvalidKey(t *testing.T) {
	ch := make(chan minio.ObjectMultipartInfo)
	close(ch)
	err := awaitInterchangeMultipartAbsent(context.Background(), "bad-key", ch)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid key must return ErrInvalid, got %v", err)
	}
}

func TestPurgeInterchangeKeyRejectsInvalidKey(t *testing.T) {
	noop := func(context.Context, string) error { return nil }
	nolist := func(context.Context, string) <-chan minio.ObjectMultipartInfo {
		ch := make(chan minio.ObjectMultipartInfo)
		close(ch)
		return ch
	}
	err := purgeInterchangeKey(context.Background(), "bad-key", noop, nolist)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid key must return ErrInvalid, got %v", err)
	}
}

// ── Store method guard paths (guard fires before nil-Store dereference) ───────

func TestCompleteInterchangeMultipartRejectsSmallSize(t *testing.T) {
	key, _ := InterchangeUploadKey(uuid.New(), uuid.New())
	var s *Store
	_, err := s.CompleteInterchangeMultipart(context.Background(), key, "id", 10, nil)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("size<22 must return ErrInvalid, got %v", err)
	}
}

func TestRecoverInterchangeMultipartRejectsSmallSize(t *testing.T) {
	key, _ := InterchangeUploadKey(uuid.New(), uuid.New())
	var s *Store
	_, err := s.RecoverInterchangeMultipart(context.Background(), key, 10)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("size<22 must return ErrInvalid, got %v", err)
	}
}

func TestVerifyInterchangePendingRejectsInvalidKey(t *testing.T) {
	var s *Store
	_, err := s.VerifyInterchangePending(context.Background(), PendingObject{Key: "bad-key"}, strings.Repeat("a", 64))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid key must return ErrInvalid, got %v", err)
	}
}

func TestRemoveRejectsInvalidObject(t *testing.T) {
	var s *Store
	err := s.Remove(context.Background(), Object{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid Object must return ErrInvalid, got %v", err)
	}
}

// ── PurgeInterchangeArchiveKey: key belongs to different tenant ───────────────

func TestPurgeInterchangeArchiveKeyRejectsMismatchedTenant(t *testing.T) {
	tid, upload := uuid.New(), uuid.New()
	otherTenant := uuid.New()
	genKey, _ := InterchangeGenerationKey(otherTenant, upload, uuid.New())
	var s *Store
	err := s.PurgeInterchangeArchiveKey(context.Background(), tid, upload, genKey)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("tenant mismatch must return ErrInvalid, got %v", err)
	}
}
