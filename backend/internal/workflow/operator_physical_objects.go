package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/objectstore"
	"strings"
)

// OperatorPhysicalObjects only reads the unique owned measurement directory.
// Its version identifier is a content digest, never a production object-store
// VersionID or trustworthy SQL receipt. It does not publish qualification rows.
type OperatorPhysicalObjects struct {
	root           string
	allowed        map[string]map[string]bool
	maxObjectBytes int64
}

func NewOperatorPhysicalObjects(repository, root string, allowed map[string]map[string]bool, maxObjectBytes int64) (*OperatorPhysicalObjects, error) {
	repo, e := filepath.EvalSymlinks(repository)
	if e != nil {
		return nil, e
	}
	dir, e := filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	rel, e := filepath.Rel(filepath.Join(repo, "work"), dir)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || maxObjectBytes < 1 {
		return nil, errors.New("operator physical root invalid")
	}
	info, e := os.Stat(dir)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("operator physical root must be private")
	}
	owned := map[string]map[string]bool{}
	for op, roles := range allowed {
		owned[op] = map[string]bool{}
		for role, value := range roles {
			if value {
				owned[op][role] = true
			}
		}
	}
	return &OperatorPhysicalObjects{dir, owned, maxObjectBytes}, nil
}
func (s *OperatorPhysicalObjects) path(key string) (string, error) {
	if key == "" || filepath.Base(key) != key || key == "." || key == ".." {
		return "", errors.New("operator object key invalid")
	}
	path := filepath.Join(s.root, key)
	info, e := os.Lstat(path)
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("operator object must be private regular file")
	}
	return path, nil
}
func (s *OperatorPhysicalObjects) Open(ctx context.Context, o objectstore.Object) (io.ReadCloser, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	path, e := s.path(o.Key)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil || info.Size() != o.Bytes || o.Bytes < 1 || o.Bytes > s.maxObjectBytes || o.VersionID != o.SHA256 {
		f.Close()
		return nil, errors.New("operator object identity changed")
	}
	return f, nil
}
func (s *OperatorPhysicalObjects) VerifyMeasuredObject(ctx context.Context, t PreparedTask, p OperatorMeasuredOutput) error {
	a := p.Artifact
	if p.Producer != t.ID || !s.allowed[t.Operation][p.Role] || a.ArtifactID.String() != a.Key || a.VersionID != a.SHA256 || a.Bucket != "operator-measurement" || a.Bytes < 1 || a.Bytes > s.maxObjectBytes {
		return errors.New("operator output tuple invalid")
	}
	if len(a.SHA256) != 64 || strings.ToLower(a.SHA256) != a.SHA256 {
		return errors.New("operator output digest invalid")
	}
	if _, e := hex.DecodeString(a.SHA256); e != nil {
		return e
	}
	source, e := s.Open(ctx, a.object())
	if e != nil {
		return e
	}
	defer source.Close()
	sum := sha256.New()
	buf := make([]byte, 65536)
	total := int64(0)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, readErr := source.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > a.Bytes {
				return errors.New("operator output grew")
			}
			sum.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	if total != a.Bytes || hex.EncodeToString(sum.Sum(nil)) != a.SHA256 {
		return errors.New("operator output physical bytes mismatch")
	}
	return nil
}

var _ ArtifactOpener = (*OperatorPhysicalObjects)(nil)
