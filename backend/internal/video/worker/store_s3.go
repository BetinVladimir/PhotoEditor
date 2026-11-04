package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Store keeps objects in an S3-compatible bucket (MinIO). Signer, when set, is a client built for the endpoint
// browsers reach; presigned URLs are made with it, everything else goes through Client.
type S3Store struct {
	Client *minio.Client
	core   *minio.Core
	Signer *minio.Client
}

// NewS3Store connects to endpoint ("host:port"); publicEndpoint may be empty.
func NewS3Store(endpoint, publicEndpoint, access, secret string, secure bool) (*S3Store, error) {
	creds := credentials.NewStaticV4(access, secret, "")
	c, err := minio.New(endpoint, &minio.Options{Creds: creds, Secure: secure})
	if err != nil {
		return nil, err
	}
	s := &S3Store{Client: c, core: &minio.Core{Client: c}, Signer: c}
	if publicEndpoint != "" && publicEndpoint != endpoint {
		if s.Signer, err = minio.New(publicEndpoint, &minio.Options{Creds: creds, Secure: secure}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *S3Store) Fetch(ctx context.Context, bucket, key, dst string) error {
	return s.Client.FGetObject(ctx, bucket, key, dst, minio.GetObjectOptions{})
}

func (s *S3Store) Put(ctx context.Context, bucket, key, src string) (string, int64, error) {
	sha, size, err := HashFile(src)
	if err != nil {
		return "", 0, err
	}
	_, err = s.Client.FPutObject(ctx, bucket, key, src, minio.PutObjectOptions{UserMetadata: map[string]string{"sha256": sha}})
	return sha, size, err
}

// Remove deletes every version of the object (the bucket is versioned, so a plain delete would only add a marker).
func (s *S3Store) Remove(ctx context.Context, bucket, key string) error {
	for o := range s.Client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: key, WithVersions: true}) {
		if o.Err != nil {
			return o.Err
		}
		if o.Key != key {
			continue
		}
		if err := s.Client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{VersionID: o.VersionID}); err != nil && minio.ToErrorResponse(err).Code != "NoSuchKey" {
			return err
		}
	}
	return nil
}

// BeginMultipart starts a multipart upload and returns its id.
func (s *S3Store) BeginMultipart(ctx context.Context, bucket, key, contentType string) (string, error) {
	return s.core.NewMultipartUpload(ctx, bucket, key, minio.PutObjectOptions{ContentType: contentType})
}

// SignPart returns a presigned PUT URL for one part.
func (s *S3Store) SignPart(ctx context.Context, bucket, key, uploadID string, part int, ttl time.Duration) (string, error) {
	q := url.Values{"partNumber": {strconv.Itoa(part)}, "uploadId": {uploadID}}
	u, err := s.Signer.Presign(ctx, "PUT", bucket, key, ttl, q)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// ListedPart is a part the store already holds.
type ListedPart struct {
	No   int
	ETag string
	Size int64
}

// ListParts returns the parts uploaded so far, by number.
func (s *S3Store) ListParts(ctx context.Context, bucket, key, uploadID string) ([]ListedPart, error) {
	var out []ListedPart
	marker := 0
	for {
		res, err := s.core.ListObjectParts(ctx, bucket, key, uploadID, marker, 1000)
		if err != nil {
			return nil, err
		}
		for _, p := range res.ObjectParts {
			out = append(out, ListedPart{No: p.PartNumber, ETag: p.ETag, Size: p.Size})
		}
		if !res.IsTruncated {
			break
		}
		marker = res.NextPartNumberMarker
	}
	sort.Slice(out, func(i, j int) bool { return out[i].No < out[j].No })
	return out, nil
}

// CompleteMultipart assembles the parts and returns the object's version id ("null" when the bucket is unversioned).
func (s *S3Store) CompleteMultipart(ctx context.Context, bucket, key, uploadID string, parts []ListedPart) (string, error) {
	cp := make([]minio.CompletePart, len(parts))
	for i, p := range parts {
		cp[i] = minio.CompletePart{PartNumber: p.No, ETag: p.ETag}
	}
	info, err := s.core.CompleteMultipartUpload(ctx, bucket, key, uploadID, cp, minio.PutObjectOptions{})
	if err != nil {
		return "", err
	}
	if info.VersionID == "" {
		return "null", nil
	}
	return info.VersionID, nil
}

func (s *S3Store) AbortMultipart(ctx context.Context, bucket, key, uploadID string) error {
	return s.core.AbortMultipartUpload(ctx, bucket, key, uploadID)
}

// HashObject streams the object and returns its sha256 and size.
func (s *S3Store) HashObject(ctx context.Context, bucket, key string) (string, int64, error) {
	o, err := s.Client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return "", 0, err
	}
	defer o.Close()
	h := sha256.New()
	n, err := io.Copy(h, o)
	if err != nil {
		return "", 0, fmt.Errorf("hash object: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// EnsureBucket creates the bucket when it does not exist and turns versioning on: artifacts pin an object version.
func (s *S3Store) EnsureBucket(ctx context.Context, bucket string) error {
	ok, err := s.Client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !ok {
		if err := s.Client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return err
		}
	}
	return s.Client.EnableVersioning(ctx, bucket)
}
