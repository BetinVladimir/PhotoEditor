package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

const (
	MaxUploadBytes = int64(536870912)
	MaxUploadParts = 10000
	PartBytes      = int64(16777216)
)

type UploadedPart struct {
	PartNumber int    `json:"part_no"`
	ETag       string `json:"etag"`
}

type PartURL struct {
	PartNumber int       `json:"part_no"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type ReadURL struct {
	URL       string
	ExpiresAt time.Time
}

type PendingObject struct {
	Key, VersionID, ContentType string
	Bytes                       int64
}

func StagingKey(tenant, upload uuid.UUID) (string, error) {
	if tenant == uuid.Nil || upload == uuid.Nil {
		return "", ErrInvalid
	}
	return "staging/tenants/" + tenant.String() + "/uploads/" + upload.String(), nil
}

func validUploadID(id string) bool {
	return id != "" && len(id) <= 1024 && !strings.ContainsAny(id, "\r\n")
}

func (s *Store) BeginMultipart(ctx context.Context, key, contentType string) (string, error) {
	if s == nil || s.client == nil || !validKey(key) || !mimePattern.MatchString(contentType) {
		return "", ErrInvalid
	}
	id, err := (minio.Core{Client: s.client}).NewMultipartUpload(ctx, s.bucket, key, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", classify(err)
	}
	if !validUploadID(id) {
		return "", ErrIntegrity
	}
	return id, nil
}

// SignUploadParts issues PUT URLs only for explicit part numbers. The public
// client signs the externally reachable Host; no internal endpoint is exposed.
func (s *Store) SignUploadParts(ctx context.Context, key, uploadID string, parts []int, ttl time.Duration) ([]PartURL, error) {
	if s == nil || s.publicClient == nil || !validKey(key) || !validUploadID(uploadID) || len(parts) < 1 || len(parts) > 100 || ttl < time.Minute || ttl > 15*time.Minute {
		return nil, ErrInvalid
	}
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		if part < 1 || part > MaxUploadParts {
			return nil, ErrInvalid
		}
		if _, exists := seen[part]; exists {
			return nil, ErrInvalid
		}
		seen[part] = struct{}{}
	}
	ordered := append([]int(nil), parts...)
	sort.Ints(ordered)
	expires := time.Now().UTC().Add(ttl)
	out := make([]PartURL, 0, len(ordered))
	for _, part := range ordered {
		query := url.Values{"partNumber": {stringInt(part)}, "uploadId": {uploadID}}
		signed, err := s.publicClient.Presign(ctx, http.MethodPut, s.bucket, key, ttl, query)
		if err != nil {
			return nil, classify(err)
		}
		out = append(out, PartURL{PartNumber: part, URL: signed.String(), ExpiresAt: expires})
	}
	return out, nil
}

// SignRead pins an immutable version. Callers must authorize the exact object
// before invoking this transport-only primitive.
func (s *Store) SignRead(ctx context.Context, object Object, ttl time.Duration) (ReadURL, error) {
	if s == nil || s.publicClient == nil || !object.valid() || ttl < time.Minute || ttl > 15*time.Minute {
		return ReadURL{}, ErrInvalid
	}
	query := url.Values{"versionId": {object.VersionID}, "response-content-disposition": {"inline"}}
	signed, err := s.publicClient.Presign(ctx, http.MethodGet, s.bucket, object.Key, ttl, query)
	if err != nil {
		return ReadURL{}, classify(err)
	}
	return ReadURL{URL: signed.String(), ExpiresAt: time.Now().UTC().Add(ttl)}, nil
}

// SignDownload pins an immutable version and forces a sanitized attachment
// filename chosen by the authorized application layer.
func (s *Store) SignDownload(ctx context.Context, object Object, filename string, ttl time.Duration) (ReadURL, error) {
	if s == nil || s.publicClient == nil || !object.valid() || ttl < time.Minute || ttl > 15*time.Minute || filename == "" || len([]byte(filename)) > 180 || strings.ContainsAny(filename, "\r\n/\\\"") {
		return ReadURL{}, ErrInvalid
	}
	query := url.Values{"versionId": {object.VersionID}, "response-content-disposition": {`attachment; filename="` + filename + `"`}}
	signed, err := s.publicClient.Presign(ctx, http.MethodGet, s.bucket, object.Key, ttl, query)
	if err != nil {
		return ReadURL{}, classify(err)
	}
	return ReadURL{URL: signed.String(), ExpiresAt: time.Now().UTC().Add(ttl)}, nil
}

func stringInt(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}

func validETag(value string) bool {
	if len(value) < 1 || len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	return true
}

func (s *Store) CompleteMultipart(ctx context.Context, key, uploadID, contentType string, expectedBytes int64, parts []UploadedPart) (PendingObject, error) {
	return s.completeMultipartBounded(ctx, key, uploadID, contentType, expectedBytes, parts, MaxUploadBytes)
}
func (s *Store) completeMultipartBounded(ctx context.Context, key, uploadID, contentType string, expectedBytes int64, parts []UploadedPart, maxBytes int64) (PendingObject, error) {
	if s == nil || s.client == nil || !validKey(key) || !validUploadID(uploadID) || !mimePattern.MatchString(contentType) || expectedBytes < 1 || expectedBytes > maxBytes || len(parts) < 1 || len(parts) > MaxUploadParts {
		return PendingObject{}, ErrInvalid
	}
	complete := make([]minio.CompletePart, len(parts))
	previous := 0
	for i, part := range parts {
		if part.PartNumber <= previous || part.PartNumber > MaxUploadParts || !validETag(part.ETag) {
			return PendingObject{}, ErrInvalid
		}
		previous = part.PartNumber
		complete[i] = minio.CompletePart{PartNumber: part.PartNumber, ETag: part.ETag}
	}
	info, err := (minio.Core{Client: s.client}).CompleteMultipartUpload(ctx, s.bucket, key, uploadID, complete, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return PendingObject{}, classify(err)
	}
	if info.VersionID == "" || info.VersionID == "null" {
		return PendingObject{}, ErrIntegrity
	}
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{VersionID: info.VersionID})
	if err != nil {
		return PendingObject{}, classify(err)
	}
	if stat.VersionID != info.VersionID || stat.Size != expectedBytes || stat.ContentType != contentType {
		return PendingObject{}, ErrIntegrity
	}
	return PendingObject{Key: key, VersionID: info.VersionID, ContentType: contentType, Bytes: stat.Size}, nil
}

// RecoverCompletedMultipart resolves a lost Complete response for a generated,
// single-owner staging key. Versioning is mandatory; an unversioned latest
// object is never accepted as the completion result.
func (s *Store) RecoverCompletedMultipart(ctx context.Context, key, contentType string, expectedBytes int64) (PendingObject, error) {
	return s.recoverCompletedMultipartBounded(ctx, key, contentType, expectedBytes, MaxUploadBytes)
}
func (s *Store) recoverCompletedMultipartBounded(ctx context.Context, key, contentType string, expectedBytes int64, maxBytes int64) (PendingObject, error) {
	if s == nil || s.client == nil || !validKey(key) || !mimePattern.MatchString(contentType) || expectedBytes < 1 || expectedBytes > maxBytes {
		return PendingObject{}, ErrInvalid
	}
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return PendingObject{}, classify(err)
	}
	if stat.VersionID == "" || stat.VersionID == "null" || stat.Size != expectedBytes || stat.ContentType != contentType {
		return PendingObject{}, ErrIntegrity
	}
	return PendingObject{Key: key, VersionID: stat.VersionID, ContentType: contentType, Bytes: stat.Size}, nil
}

func (s *Store) AbortMultipart(ctx context.Context, key, uploadID string) error {
	if s == nil || s.client == nil || !validKey(key) || !validUploadID(uploadID) {
		return ErrInvalid
	}
	return classify((minio.Core{Client: s.client}).AbortMultipartUpload(ctx, s.bucket, key, uploadID))
}

// VerifyPending pins the completed version, reads every byte and computes the
// canonical SHA-256. expectedSHA may be empty; a supplied value is a check, not
// trusted metadata. ETag is deliberately ignored.
func (s *Store) VerifyPending(ctx context.Context, pending PendingObject, expectedSHA string) (Object, error) {
	return s.verifyPendingBounded(ctx, pending, expectedSHA, MaxUploadBytes)
}
func (s *Store) verifyPendingBounded(ctx context.Context, pending PendingObject, expectedSHA string, maxBytes int64) (Object, error) {
	if s == nil || s.client == nil || !validKey(pending.Key) || pending.VersionID == "" || pending.VersionID == "null" || pending.Bytes < 1 || pending.Bytes > maxBytes || !mimePattern.MatchString(pending.ContentType) || (expectedSHA != "" && !hashPattern.MatchString(expectedSHA)) {
		return Object{}, ErrInvalid
	}
	body, err := s.client.GetObject(ctx, s.bucket, pending.Key, minio.GetObjectOptions{VersionID: pending.VersionID, Checksum: true})
	if err != nil {
		return Object{}, classify(err)
	}
	defer body.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(body, pending.Bytes+1))
	if err != nil {
		return Object{}, classify(err)
	}
	sha := hex.EncodeToString(hash.Sum(nil))
	if written != pending.Bytes {
		return Object{}, ErrIntegrity
	}
	object := Object{Key: pending.Key, VersionID: pending.VersionID, SHA256: sha, ContentType: pending.ContentType, Bytes: pending.Bytes}
	if !object.valid() {
		return Object{}, ErrIntegrity
	}
	if expectedSHA != "" && sha != expectedSHA {
		return object, ErrIntegrity
	}
	return object, nil
}

// isMultipartSourceKey admits only the canonical key produced by StagingKey.
// Other objects still require the metadata SHA set by PutImmutable.
func isMultipartSourceKey(key string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 5 || parts[0] != "staging" || parts[1] != "tenants" || parts[3] != "uploads" {
		return false
	}
	tenant, e := uuid.Parse(parts[2])
	if e != nil {
		return false
	}
	upload, e := uuid.Parse(parts[4])
	if e != nil {
		return false
	}
	canonical, e := StagingKey(tenant, upload)
	return e == nil && key == canonical
}
