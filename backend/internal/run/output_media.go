package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"time"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

type OutputReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

// At most two verified output bodies may occupy API scratch at once. This is
// process-local; deployment capacity must also budget the shared temp volume.
var outputMediaSlots = make(chan struct{}, 2)

type VerifiedOutputMedia struct {
	File    *os.File
	Output  Output
	release func()
}

func (m *VerifiedOutputMedia) Close() error {
	if m == nil || m.File == nil {
		return nil
	}
	err := m.File.Close()
	m.File = nil
	if m.release != nil {
		m.release()
		m.release = nil
	}
	return err
}

func outputGrantMatches(grant OutputGrant, candidate pendingOutput) bool {
	return candidate.Revocable && candidate.ArtifactID == grant.Artifact &&
		candidate.Role == grant.Role && candidate.SHA256 == grant.SHA256 &&
		candidate.Object.Key != "" && outputObjectKeySHA(candidate.Object.Key) == grant.ObjectKeySHA &&
		candidate.Object.VersionID == grant.ObjectVersion &&
		candidate.MediaType == grant.MediaType && candidate.Bytes == grant.Bytes
}

func outputCandidateSetEqual(first, second []pendingOutput) bool {
	if len(first) != len(second) {
		return false
	}
	counts := make(map[pendingOutput]int, len(first))
	for _, item := range first {
		counts[item]++
	}
	for _, item := range second {
		counts[item]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func verifiedOutputCopy(ctx context.Context, writer io.Writer, reader io.Reader, expected int64, digest hash.Hash) error {
	if expected < 2 || expected > 4*1024*1024*1024 {
		return objectstore.ErrInvalid
	}
	buffer := make([]byte, 1024*1024)
	limited := io.LimitReader(reader, expected+1)
	written, err := io.CopyBuffer(io.MultiWriter(writer, digest), &contextReader{ctx: ctx, reader: limited}, buffer)
	if err != nil {
		return err
	}
	if written != expected {
		return objectstore.ErrIntegrity
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if cancelled := r.ctx.Err(); cancelled != nil {
		return 0, cancelled
	}
	return n, err
}

// ReadRevocableOutput checks fresh SQL authority, verifies all physical bytes
// before exposure, then repeats the exact authorized output set. The returned
// file and its slot must be closed by the HTTP caller on every path.
func (s Service) ReadRevocableOutput(ctx context.Context, key []byte, token string, now time.Time) (*VerifiedOutputMedia, error) {
	grant, err := OpenOutputGrant(key, token, now)
	if err != nil {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	if s.ReadObjects == nil || s.DB == nil || s.Bucket == "" {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	select {
	case outputMediaSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	releaseSlot := func() { <-outputMediaSlots }
	who := auth.Identity{UserID: grant.User, SessionID: grant.Session}
	first, err := s.readOutputCandidates(ctx, who, grant.Tenant, grant.Run)
	if err != nil {
		releaseSlot()
		return nil, err
	}
	var selected *pendingOutput
	for i := range first {
		if outputGrantMatches(grant, first[i]) {
			if selected != nil {
				releaseSlot()
				return nil, objectstore.ErrIntegrity
			}
			selected = &first[i]
		}
	}
	if selected == nil {
		releaseSlot()
		return nil, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	file, err := os.CreateTemp("", "photo-output-verified-*")
	if err != nil {
		releaseSlot()
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if removeErr := os.Remove(file.Name()); removeErr != nil {
		_ = file.Close()
		releaseSlot()
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	cleanup := func() { _ = file.Close(); releaseSlot() }
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	reader, openErr := s.ReadObjects.Open(ctx, selected.Object)
	if openErr != nil {
		err = openErr
		return nil, err
	}
	digest := sha256.New()
	err = verifiedOutputCopy(ctx, file, reader, selected.Bytes, digest)
	closeErr := reader.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if hex.EncodeToString(digest.Sum(nil)) != selected.SHA256 {
		err = objectstore.ErrIntegrity
		return nil, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	second, err := s.readOutputCandidates(ctx, who, grant.Tenant, grant.Run)
	if err != nil {
		return nil, err
	}
	if !outputCandidateSetEqual(first, second) || time.Now().Unix() >= grant.ExpiresUnix {
		err = domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &VerifiedOutputMedia{File: file, Output: selected.Output,
		release: releaseSlot}, nil
}
