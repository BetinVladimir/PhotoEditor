// Package objectstore owns S3 locations; public and worker APIs expose only IDs.
package objectstore

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/s3utils"
	"hash"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid object reference")
var ErrIntegrity = errors.New("object integrity mismatch")
var ErrExists = errors.New("immutable object already exists")
var ErrUnavailable = errors.New("object storage unavailable")
var ErrMissing = errors.New("object missing")

type StorageError struct {
	Code   string
	Status int
}

func (e *StorageError) Error() string {
	return fmt.Sprintf("object storage unavailable (%s/%d)", e.Code, e.Status)
}
func (e *StorageError) Unwrap() error { return ErrUnavailable }

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/_.-]*$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var mimePattern = regexp.MustCompile(`^[a-z0-9.+-]+/[a-z0-9.+-]+$`)

type Config struct {
	Endpoint, PublicEndpoint, Bucket, AccessKey, SecretKey, Region string
	Development                                                    bool
}
type Store struct {
	client       *minio.Client
	publicClient *minio.Client
	bucket       string
}
type Object struct {
	Key, VersionID, SHA256, ContentType string
	Bytes                               int64
}

func validKey(key string) bool {
	return len(key) <= 1024 && keyPattern.MatchString(key) && path.Clean(key) == key && !strings.HasSuffix(key, "/")
}
func (o Object) valid() bool {
	return validKey(o.Key) && o.VersionID != "" && o.VersionID != "null" && hashPattern.MatchString(o.SHA256) && mimePattern.MatchString(o.ContentType) && o.Bytes >= 0
}
func AttemptKey(tenant, task, artifact uuid.UUID, attempt int, fence int64) (string, error) {
	if tenant == uuid.Nil || task == uuid.Nil || artifact == uuid.Nil || attempt < 1 || attempt > 6 || fence < 1 {
		return "", ErrInvalid
	}
	return fmt.Sprintf("tenants/%s/tasks/%s/attempts/%d/fences/%d/%s", tenant, task, attempt, fence, artifact), nil
}
func New(c Config) (*Store, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || c.AccessKey == "" || c.SecretKey == "" || s3utils.CheckValidBucketNameStrict(c.Bucket) != nil {
		return nil, ErrInvalid
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if !c.Development || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, ErrInvalid
		}
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	if c.PublicEndpoint == "" {
		c.PublicEndpoint = c.Endpoint
	}
	publicURL, err := url.Parse(c.PublicEndpoint)
	if err != nil || publicURL.Host == "" || publicURL.User != nil || publicURL.Path != "" || publicURL.RawQuery != "" || publicURL.Fragment != "" || (publicURL.Scheme != "https" && publicURL.Scheme != "http") {
		return nil, ErrInvalid
	}
	if publicURL.Scheme == "http" {
		ip := net.ParseIP(publicURL.Hostname())
		if !c.Development || (publicURL.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, ErrInvalid
		}
	}
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 32, MaxIdleConnsPerHost: 16}
	creds := credentials.NewStaticV4(c.AccessKey, c.SecretKey, "")
	client, err := minio.New(u.Host, &minio.Options{Creds: creds, Secure: u.Scheme == "https", Region: c.Region, Transport: transport, TrailingHeaders: true})
	if err != nil {
		return nil, ErrInvalid
	}
	publicClient, err := minio.New(publicURL.Host, &minio.Options{Creds: creds, Secure: publicURL.Scheme == "https", Region: c.Region, Transport: transport, TrailingHeaders: true})
	if err != nil {
		return nil, ErrInvalid
	}
	return &Store{client: client, publicClient: publicClient, bucket: c.Bucket}, nil
}

// Bootstrap is privileged and explicit; normal runtime never creates buckets.
func (s *Store) Bootstrap(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return classify(err)
	}
	if !exists {
		if err = s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return classify(err)
		}
	}
	if err = s.client.SetBucketVersioning(ctx, s.bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); err != nil {
		return classify(err)
	}
	status, err := s.client.GetBucketVersioning(ctx, s.bucket)
	if err != nil {
		return classify(err)
	}
	if !status.Enabled() {
		return ErrInvalid
	}
	return nil
}
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrIntegrity
	}
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchKey", "NoSuchVersion", "NoSuchBucket":
		return ErrMissing
	case "PreconditionFailed", "ConditionalRequestConflict":
		return ErrExists
	case "BadDigest", "XAmzContentSHA256Mismatch", "IncompleteBody":
		return ErrIntegrity
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	response := minio.ToErrorResponse(err)
	code := response.Code
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`).MatchString(code) {
		code = "transport"
	}
	return &StorageError{Code: code, Status: response.StatusCode}
}

type countingHash struct {
	reader io.Reader
	hash   hash.Hash
	count  int64
}

func (r *countingHash) Read(p []byte) (int, error) {
	n, e := r.reader.Read(p)
	if n > 0 {
		r.hash.Write(p[:n])
		r.count += int64(n)
	}
	return n, e
}

// PutImmutable requires a generated attempt key and the expected full SHA256.
// If-None-Match applies to single-part PUT and multipart completion. A failed
// verification removes only the version created by this call, never the key's
// latest version or an object belonging to another attempt.
func (s *Store) PutImmutable(ctx context.Context, key string, body io.Reader, size int64, sha, contentType string) (Object, error) {
	if !validKey(key) || body == nil || size < 0 || size > 1<<50 || !hashPattern.MatchString(sha) || !mimePattern.MatchString(contentType) {
		return Object{}, ErrInvalid
	}
	reader := &countingHash{reader: io.LimitReader(body, size), hash: sha256.New()}
	opts := minio.PutObjectOptions{ContentType: contentType, UserMetadata: map[string]string{"sha256": sha}, NumThreads: 1, PartSize: 8 * 1024 * 1024, Checksum: minio.ChecksumSHA256}
	opts.SetMatchETagExcept("*")
	uploaded, err := s.client.PutObject(ctx, s.bucket, key, reader, size, opts)
	if err != nil {
		return Object{}, classify(err)
	}
	object := Object{Key: key, VersionID: uploaded.VersionID, SHA256: sha, ContentType: contentType, Bytes: size}
	// Require EOF, including when the SDK read exactly Content-Length.
	extra := make([]byte, 1)
	n, readErr := io.ReadFull(body, extra)
	if reader.count != size || n != 0 || readErr != io.EOF || hex.EncodeToString(reader.hash.Sum(nil)) != sha || !object.valid() {
		s.discardCreated(object)
		return Object{}, ErrIntegrity
	}
	if err = s.Check(ctx, object); err != nil {
		s.discardCreated(object)
		return Object{}, err
	}
	return object, nil
}
func (s *Store) discardCreated(object Object) {
	if object.VersionID == "" || object.VersionID == "null" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.client.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID})
}
func (s *Store) Check(ctx context.Context, object Object) error {
	if !object.valid() {
		return ErrInvalid
	}
	stat, err := s.client.StatObject(ctx, s.bucket, object.Key, minio.StatObjectOptions{VersionID: object.VersionID})
	if err != nil {
		return classify(err)
	}
	if stat.VersionID != object.VersionID || stat.Size != object.Bytes || stat.ContentType != object.ContentType || stat.Metadata.Get("X-Amz-Meta-Sha256") != object.SHA256 {
		return ErrIntegrity
	}
	return nil
}

// Open pins the exact immutable object version and verifies the full digest at
// EOF. A caller that stops early must not treat that as integrity verification.
func (s *Store) Open(ctx context.Context, object Object) (io.ReadCloser, error) {
	if err := s.Check(ctx, object); err != nil {
		return nil, err
	}
	body, err := s.client.GetObject(ctx, s.bucket, object.Key, minio.GetObjectOptions{VersionID: object.VersionID, Checksum: true})
	if err != nil {
		return nil, classify(err)
	}
	return &verifiedReader{body: body, hash: sha256.New(), expected: object}, nil
}

type verifiedReader struct {
	body     io.ReadCloser
	hash     hash.Hash
	expected Object
	count    int64
	done     bool
}

func (r *verifiedReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n, err := r.body.Read(p)
	if n > 0 {
		r.count += int64(n)
		r.hash.Write(p[:n])
	}
	if r.count > r.expected.Bytes {
		return n, ErrIntegrity
	}
	if err == io.EOF {
		r.done = true
		if r.count != r.expected.Bytes || hex.EncodeToString(r.hash.Sum(nil)) != r.expected.SHA256 {
			return n, ErrIntegrity
		}
	}
	if err != nil && err != io.EOF {
		return n, classify(err)
	}
	return n, err
}
func (r *verifiedReader) Close() error { return r.body.Close() }

// Remove deletes a specific version only; GC authorization belongs to the DB.
func (s *Store) Remove(ctx context.Context, object Object) error {
	if !object.valid() {
		return ErrInvalid
	}
	return classify(s.client.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID}))
}

// OpenRange pins the version and returns only an explicit bounded interval.
// The response is partial: its bytes cannot validate the full-object SHA256.
func (s *Store) OpenRange(ctx context.Context, object Object, start, end int64) (io.ReadCloser, error) {
	if start < 0 || end < start || end >= object.Bytes || end-start >= 16*1024*1024 {
		return nil, ErrInvalid
	}
	if err := s.Check(ctx, object); err != nil {
		return nil, err
	}
	options := minio.GetObjectOptions{VersionID: object.VersionID}
	if err := options.SetRange(start, end); err != nil {
		return nil, ErrInvalid
	}
	body, err := s.client.GetObject(ctx, s.bucket, object.Key, options)
	if err != nil {
		return nil, classify(err)
	}
	return body, nil
}

// Ready checks existing bucket versioning without creating or modifying storage.
func (s *Store) Ready(ctx context.Context) error {
	status, err := s.client.GetBucketVersioning(ctx, s.bucket)
	if err != nil {
		return classify(err)
	}
	if !status.Enabled() {
		return ErrInvalid
	}
	return nil
}

// PurgeAttempt removes only the exact generated key from an authorized orphan
// ticket. It does not accept a user path or a bucket prefix. The caller must hold
// a cleanup lease and exclude every canonical artifact before invoking it.
func (s *Store) PurgeAttempt(ctx context.Context, tenant, task, artifact uuid.UUID, attempt int, fence int64) error {
	key, err := AttemptKey(tenant, task, artifact, attempt, fence)
	if err != nil {
		return err
	}
	return s.purgeReservedKey(ctx, key)
}

func (s *Store) purgeReservedKey(ctx context.Context, key string) error {
	if err := s.client.RemoveIncompleteUpload(ctx, s.bucket, key); err != nil {
		return classify(err)
	}
	listing, cancel := context.WithCancel(ctx)
	defer cancel()
	removed := 0
	for object := range s.client.ListObjects(listing, s.bucket, minio.ListObjectsOptions{Prefix: key, Recursive: true, WithVersions: true}) {
		if object.Err != nil {
			return classify(object.Err)
		}
		if object.Key != key {
			continue
		}
		if object.VersionID == "" || object.VersionID == "null" {
			return ErrInvalid
		}
		if removed >= 64 {
			return ErrUnavailable
		}
		if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{VersionID: object.VersionID}); err != nil {
			return classify(err)
		}
		removed++
	}
	for object := range s.client.ListObjects(listing, s.bucket, minio.ListObjectsOptions{Prefix: key, Recursive: true, WithVersions: true}) {
		if object.Err != nil {
			return classify(object.Err)
		}
		if object.Key == key {
			return ErrUnavailable
		}
	}
	return nil
}

func tenantPrefix(tenant uuid.UUID) (string, error) {
	if tenant == uuid.Nil {
		return "", ErrInvalid
	}
	return "tenants/" + tenant.String() + "/", nil
}

// PurgeTenantBatch is available only to the maintenance service. It removes a
// bounded number of exact versions and incomplete uploads below one UUID-derived
// tenant prefix. Returning true requires another pass; false means both listings
// were observed empty after deletion.
func (s *Store) PurgeTenantBatch(ctx context.Context, tenant uuid.UUID, limit int) (bool, error) {
	prefix, err := tenantPrefix(tenant)
	if err != nil || limit < 1 || limit > 256 {
		return false, ErrInvalid
	}
	removed := 0
	for upload := range s.client.ListIncompleteUploads(ctx, s.bucket, prefix, true) {
		if upload.Err != nil {
			return false, classify(upload.Err)
		}
		if !strings.HasPrefix(upload.Key, prefix) || !validKey(upload.Key) {
			return false, ErrInvalid
		}
		if err = s.client.RemoveIncompleteUpload(ctx, s.bucket, upload.Key); err != nil {
			return false, classify(err)
		}
		removed++
		if removed >= limit {
			return true, nil
		}
	}
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true}) {
		if object.Err != nil {
			return false, classify(object.Err)
		}
		if !strings.HasPrefix(object.Key, prefix) || !validKey(object.Key) || object.VersionID == "" || object.VersionID == "null" {
			return false, ErrInvalid
		}
		if err = s.client.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID}); err != nil {
			return false, classify(err)
		}
		removed++
		if removed >= limit {
			return true, nil
		}
	}
	// A second observation closes races with listing pagination and delayed
	// version visibility. The SQL fence prevents new tenant work before claim.
	for upload := range s.client.ListIncompleteUploads(ctx, s.bucket, prefix, true) {
		if upload.Err != nil {
			return false, classify(upload.Err)
		}
		return true, nil
	}
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true}) {
		if object.Err != nil {
			return false, classify(object.Err)
		}
		return true, nil
	}
	return false, nil
}
