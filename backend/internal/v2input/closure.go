// Package v2input constructs the bounded task-scoped input transport used by
// V2 workers. It does no database or object IO itself; the trusted compiler
// supplies a frozen, sorted cursor and an immutable document publisher.
package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"regexp"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

const (
	MaxEntries = 1_000_000
	ChunkSize  = 512
	MaxChunks  = 1954
)

var ErrInvalid = errors.New("invalid V2 input closure")
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Ref struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}

type Entry struct {
	Ref      Ref    `json:"ref"`
	Contract string `json:"contract"`
}

type Chunk struct {
	SchemaVersion int     `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}

type Manifest struct {
	SchemaVersion int       `json:"schema_version"`
	TaskID        uuid.UUID `json:"task_id"`
	EntryCount    int       `json:"entry_count"`
	EntriesSHA    string    `json:"entries_sha"`
	Chunks        []Ref     `json:"chunks"`
}

// Cursor must return entries sorted by UUID bytes, SHA and contract. The
// compiler obtains it from one frozen snapshot, never from a live project scan.
type Cursor interface {
	Next(context.Context) (Entry, bool, error)
}

// Publisher pins immutable JSON bytes before returning an exact content ref.
// Ordinal is the transport chunk number, or len(chunks) for the manifest.
// Calls happen outside the admission transaction and must be replay-safe.
type Publisher interface {
	Publish(context.Context, uuid.UUID, int, string, []byte) (Ref, error)
}

type Built struct {
	Manifest Ref
	Chunks   []Ref
	Count    int
	SHA256   string
}

func validRef(ref Ref) bool {
	return ref.ArtifactID != uuid.Nil && digestPattern.MatchString(ref.SHA256)
}

func validEntry(entry Entry) bool {
	return validRef(entry.Ref) && len(entry.Contract) >= 1 && len(entry.Contract) <= 120
}

func publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, value any, out Publisher) (Ref, error) {
	body, err := canonical.Marshal(value)
	if err != nil || len(body) < 2 || len(body) > 1_048_576 {
		return Ref{}, ErrInvalid
	}
	ref, err := out.Publish(ctx, task, ordinal, contract, body)
	if err != nil {
		return Ref{}, err
	}
	sum := sha256.Sum256(body)
	if !validRef(ref) || ref.SHA256 != hex.EncodeToString(sum[:]) {
		return Ref{}, ErrInvalid
	}
	return ref, nil
}

func writeEntryDigest(h hash.Hash, entry Entry, first bool) error {
	if !first {
		_, _ = h.Write([]byte(","))
	}
	data, err := canonical.Marshal(entry)
	if err != nil {
		return err
	}
	_, _ = h.Write(data)
	return nil
}

// BuildInputClosure emits at most 512 entries per chunk and retains at most
// 1954 refs. The caller must later re-read the frozen cursor in its admission
// transaction, insert all entry and transport rows, and compare this digest.
func BuildInputClosure(ctx context.Context, task uuid.UUID, cursor Cursor, out Publisher) (Built, error) {
	if task == uuid.Nil || cursor == nil || out == nil {
		return Built{}, ErrInvalid
	}
	var result Built
	var batch []Entry
	var previous Entry
	var havePrevious bool
	h := sha256.New()
	_, _ = h.Write([]byte("["))
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		ref, err := publish(ctx, task, len(result.Chunks), "InputClosureChunk",
			Chunk{SchemaVersion: 1, Entries: batch}, out)
		if err != nil {
			return err
		}
		result.Chunks = append(result.Chunks, ref)
		batch = nil
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return Built{}, err
		}
		entry, ok, err := cursor.Next(ctx)
		if err != nil {
			return Built{}, err
		}
		if !ok {
			break
		}
		if !validEntry(entry) || result.Count == MaxEntries {
			return Built{}, ErrInvalid
		}
		if havePrevious {
			cmp := bytes.Compare(previous.Ref.ArtifactID[:], entry.Ref.ArtifactID[:])
			if cmp >= 0 { // one artifact cannot appear with two hashes or contracts
				return Built{}, ErrInvalid
			}
		}
		if err := writeEntryDigest(h, entry, result.Count == 0); err != nil {
			return Built{}, err
		}
		batch = append(batch, entry)
		previous = entry
		havePrevious = true
		result.Count++
		if len(batch) == ChunkSize {
			if err := flush(); err != nil {
				return Built{}, err
			}
		}
	}
	if result.Count == 0 {
		return Built{}, ErrInvalid
	}
	if err := flush(); err != nil {
		return Built{}, err
	}
	if len(result.Chunks) > MaxChunks {
		return Built{}, ErrInvalid
	}
	_, _ = h.Write([]byte("]"))
	result.SHA256 = hex.EncodeToString(h.Sum(nil))
	manifest := Manifest{SchemaVersion: 1, TaskID: task, EntryCount: result.Count,
		EntriesSHA: result.SHA256, Chunks: result.Chunks}
	var err error
	result.Manifest, err = publish(ctx, task, len(result.Chunks), "InputClosureManifest", manifest, out)
	if err != nil {
		return Built{}, err
	}
	seen := map[uuid.UUID]struct{}{result.Manifest.ArtifactID: {}}
	for _, chunk := range result.Chunks {
		if _, exists := seen[chunk.ArtifactID]; exists {
			return Built{}, ErrInvalid
		}
		seen[chunk.ArtifactID] = struct{}{}
	}
	return result, nil
}
