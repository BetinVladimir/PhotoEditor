package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Store is the object storage the worker reads sources from and writes outputs to.
// DEMO uses LocalStore; MinIO is a drop-in later.
type Store interface {
	// Fetch copies the object to dst.
	Fetch(ctx context.Context, bucket, key, dst string) error
	// Put uploads src and returns its sha256 and size.
	Put(ctx context.Context, bucket, key, src string) (sha string, size int64, err error)
	// Remove deletes the object; a missing object is not an error.
	Remove(ctx context.Context, bucket, key string) error
}

// LocalStore keeps objects under Root/<bucket>/<key>.
type LocalStore struct{ Root string }

func (s LocalStore) path(bucket, key string) (string, error) {
	if bucket == "" || strings.Contains(bucket, "/") || strings.Contains(bucket, "..") || key == "" || strings.HasPrefix(key, "/") {
		return "", errors.New("invalid object name")
	}
	for _, p := range strings.Split(key, "/") {
		if p == "" || p == "." || p == ".." {
			return "", errors.New("invalid object name")
		}
	}
	return filepath.Join(s.Root, bucket, filepath.FromSlash(key)), nil
}

func (s LocalStore) Fetch(ctx context.Context, bucket, key, dst string) error {
	p, err := s.path(bucket, key)
	if err != nil {
		return err
	}
	return copyFile(ctx, p, dst)
}

func (s LocalStore) Put(ctx context.Context, bucket, key, src string) (string, int64, error) {
	p, err := s.path(bucket, key)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return "", 0, err
	}
	tmp := p + ".part"
	if err := copyFile(ctx, src, tmp); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", 0, err
	}
	return HashFile(p)
}

func copyFile(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, ctxReader{ctx, in}); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// HashFile returns the hex sha256 and size of a file.
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// Path returns the local file of an object, for serving with Range support.
func (s LocalStore) Path(bucket, key string) (string, error) { return s.path(bucket, key) }

// Remove deletes the object file; removing a missing object succeeds.
func (s LocalStore) Remove(_ context.Context, bucket, key string) error {
	p, err := s.path(bucket, key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
