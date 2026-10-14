package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/rawdevelopment"
)

var ErrCompletionLeaseLost = errors.New("upload completion lease lost")

type CompletionLease struct {
 RAWDevelopment *rawdevelopment.Policy
	UploadID, Token, TenantID, ArtifactID, SnapshotID, RunID uuid.UUID
	ObjectKey, MultipartID, MediaType, Filename              string
	AssumeProfile                                            *string
	ExpectedBytes                                            int64
	ExpectedSHA                                              *string
	Parts                                                    []objectstore.UploadedPart
}

type CompletionLeases interface {
	Claim(context.Context) (*CompletionLease, error)
	Renew(context.Context, CompletionLease) (bool, error)
	Release(context.Context, CompletionLease) (bool, error)
	Finish(context.Context, CompletionLease, objectstore.Object, json.RawMessage, string, json.RawMessage, string) (bool, error)
	Fail(context.Context, CompletionLease, objectstore.Object, json.RawMessage, string, json.RawMessage, string) (bool, error)
}

type CompletionObjects interface {
	CompleteMultipart(context.Context, string, string, string, int64, []objectstore.UploadedPart) (objectstore.PendingObject, error)
	RecoverCompletedMultipart(context.Context, string, string, int64) (objectstore.PendingObject, error)
	VerifyPending(context.Context, objectstore.PendingObject, string) (objectstore.Object, error)
}

type SQLCompletionLeases struct {
	DB     artifactjson.RowQuerier
	Bucket string
}

func (s SQLCompletionLeases) Claim(ctx context.Context) (*CompletionLease, error) {
	if s.DB == nil {
		return nil, errors.New("upload completion database unavailable")
	}
	lease := new(CompletionLease)
	var raw []byte
 var rawPolicy []byte
	err := s.DB.QueryRow(ctx, `SELECT upload_id,completion_token,tenant_id,object_key,multipart_id,media_type,filename,assume_profile,
	 expected_bytes,expected_sha,artifact_id,snapshot_id,run_id,parts,raw_development FROM photo.claim_upload_completion_v2()`).Scan(
		&lease.UploadID, &lease.Token, &lease.TenantID, &lease.ObjectKey, &lease.MultipartID, &lease.MediaType, &lease.Filename,
		&lease.AssumeProfile, &lease.ExpectedBytes, &lease.ExpectedSHA, &lease.ArtifactID, &lease.SnapshotID, &lease.RunID, &raw, &rawPolicy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lease.RAWDevelopment, err = decodeCompletionRAWPolicy(rawPolicy)
	if err != nil {
		return nil, err
	}
	if lease.UploadID == uuid.Nil || lease.Token == uuid.Nil || lease.TenantID == uuid.Nil || lease.ArtifactID == uuid.Nil || lease.SnapshotID == uuid.Nil || lease.RunID == uuid.Nil || lease.ArtifactID == lease.SnapshotID || lease.ArtifactID == lease.RunID || lease.SnapshotID == lease.RunID || lease.ExpectedBytes < 1 || lease.ExpectedBytes > objectstore.MaxUploadBytes || !validCompletionSource(*lease) {
		return nil, errors.New("invalid upload completion lease")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&lease.Parts) != nil || len(lease.Parts) < 1 || len(lease.Parts) > objectstore.MaxUploadParts {
		return nil, errors.New("invalid persisted upload parts")
	}
	for i, part := range lease.Parts {
		if part.PartNumber != i+1 || len(part.ETag) < 1 || len(part.ETag) > 256 || strings.ContainsAny(part.ETag, "\r\n") {
			return nil, errors.New("invalid persisted upload parts")
		}
	}
	return lease, nil
}

func (s SQLCompletionLeases) Renew(ctx context.Context, lease CompletionLease) (bool, error) {
	return s.update(ctx, "SELECT photo.renew_upload_completion($1,$2)", lease)
}
func (s SQLCompletionLeases) Release(ctx context.Context, lease CompletionLease) (bool, error) {
	return s.update(ctx, "SELECT photo.release_upload_completion($1,$2)", lease)
}
func (s SQLCompletionLeases) update(ctx context.Context, query string, lease CompletionLease) (bool, error) {
	if s.DB == nil || lease.UploadID == uuid.Nil || lease.Token == uuid.Nil {
		return false, errors.New("invalid upload completion lease")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, query, lease.UploadID, lease.Token).Scan(&ok)
	return ok, err
}
func (s SQLCompletionLeases) Finish(ctx context.Context, lease CompletionLease, object objectstore.Object, payload json.RawMessage, snapshotDigest string, config json.RawMessage, configHash string) (bool, error) {
	if s.DB == nil || s.Bucket == "" || object.Key != lease.ObjectKey || object.Bytes != lease.ExpectedBytes || object.ContentType != lease.MediaType {
		return false, errors.New("invalid upload completion")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, "SELECT photo.finish_upload_completion($1,$2,$3,$4,$5,$6,$7,$8,$9)", lease.UploadID, lease.Token, s.Bucket, object.VersionID, object.SHA256, payload, snapshotDigest, config, configHash).Scan(&ok)
	return ok, err
}
func (s SQLCompletionLeases) Fail(ctx context.Context, lease CompletionLease, object objectstore.Object, payload json.RawMessage, snapshotDigest string, config json.RawMessage, configHash string) (bool, error) {
	if s.DB == nil || s.Bucket == "" || object.Key != lease.ObjectKey || object.Bytes != lease.ExpectedBytes || object.ContentType != lease.MediaType {
		return false, errors.New("invalid failed upload completion")
	}
	var ok bool
	err := s.DB.QueryRow(ctx, "SELECT photo.fail_upload_completion($1,$2,$3,$4,$5,$6,$7,$8,$9)", lease.UploadID, lease.Token, s.Bucket, object.VersionID, object.SHA256, payload, snapshotDigest, config, configHash).Scan(&ok)
	return ok, err
}

type CompletionProcessor struct {
	Leases                                       CompletionLeases
	Objects                                      CompletionObjects
	HeartbeatInterval, RenewTimeout, WorkTimeout time.Duration
}

func completionDocuments(lease CompletionLease, sha string) (json.RawMessage, string, json.RawMessage, string, error) {
	if !validCompletionSource(lease) {
		return nil, "", nil, "", errors.New("invalid upload completion source")
	}
	suffix := strings.ToLower(filepath.Ext(lease.Filename))
    if !rawdevelopment.Valid(lease.RAWDevelopment) {return nil,"",nil,"",errors.New("invalid RAW policy")}
    version:=1
    if lease.RAWDevelopment!=nil {version=2}
	payload, err := canonical.Marshal(struct {
		SchemaVersion int     `json:"schema_version"`
		Suffix        string  `json:"suffix"`
		AssumeProfile *string `json:"assume_profile"`
		Analyze       bool    `json:"analyze"`
		Tile          int     `json:"tile"`
        RAWDevelopment *rawdevelopment.Policy `json:"raw_development,omitempty"`
	}{version, suffix, lease.AssumeProfile, true, 512, lease.RAWDevelopment})
	if err != nil {
		return nil, "", nil, "", err
	}
	snapshotDigest, err := canonical.Hash(struct {
		ArtifactID uuid.UUID       `json:"artifact_id"`
		SHA256     string          `json:"sha256"`
		Payload    json.RawMessage `json:"payload"`
	}{lease.ArtifactID, sha, payload})
	if err != nil {
		return nil, "", nil, "", err
	}
	config, err := canonical.Marshal(map[string]any{"schema_version": 1, "source": "upload"})
	if err != nil {
		return nil, "", nil, "", err
	}
	configHash, err := canonical.Hash(json.RawMessage(config))
	return payload, snapshotDigest, config, configHash, err
}

func (p CompletionProcessor) ProcessOne(ctx context.Context) (bool, error) {
	interval, renewTimeout, workTimeout := p.HeartbeatInterval, p.RenewTimeout, p.WorkTimeout
	if interval == 0 {
		interval = 10 * time.Second
	}
	if renewTimeout == 0 {
		renewTimeout = 5 * time.Second
	}
	if workTimeout == 0 {
		workTimeout = 10 * time.Minute
	}
	if p.Leases == nil || p.Objects == nil || interval <= 0 || interval > 10*time.Second || renewTimeout <= 0 || renewTimeout > 5*time.Second || workTimeout <= 0 || workTimeout > 10*time.Minute {
		return false, errors.New("invalid upload completion processor")
	}
	claim, cancel := context.WithTimeout(ctx, 5*time.Second)
	lease, err := p.Leases.Claim(claim)
	cancel()
	if err != nil || lease == nil {
		return false, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		release, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		_, _ = p.Leases.Release(release, *lease)
	}()
	if !validCompletionSource(*lease) {
		return false, errors.New("invalid upload completion source")
	}
	work, stopWork := context.WithTimeout(ctx, workTimeout)
	defer stopWork()
	work, loseLease := context.WithCancelCause(work)
	renew := func() error {
		check, done := context.WithTimeout(work, renewTimeout)
		defer done()
		ok, err := p.Leases.Renew(check, *lease)
		if err != nil {
			return err
		}
		if !ok {
			return ErrCompletionLeaseLost
		}
		return nil
	}
	if err = renew(); err != nil {
		loseLease(err)
		return false, err
	}
	heartbeat, stopHeartbeat := context.WithCancel(work)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeat.Done():
				return
			case <-ticker.C:
				if err := renew(); err != nil {
					loseLease(err)
					return
				}
			}
		}
	}()
	pending, completeErr := p.Objects.CompleteMultipart(work, lease.ObjectKey, lease.MultipartID, lease.MediaType, lease.ExpectedBytes, lease.Parts)
	if completeErr != nil && work.Err() == nil {
		pending, completeErr = p.Objects.RecoverCompletedMultipart(work, lease.ObjectKey, lease.MediaType, lease.ExpectedBytes)
	}
	var object objectstore.Object
	if completeErr == nil {
		expected := ""
		if lease.ExpectedSHA != nil {
			expected = *lease.ExpectedSHA
		}
		object, completeErr = p.Objects.VerifyPending(work, pending, expected)
	}
	stopHeartbeat()
	<-joined
	if cause := context.Cause(work); cause != nil {
		return false, cause
	}
	if completeErr != nil && errors.Is(completeErr, objectstore.ErrIntegrity) && lease.ExpectedSHA != nil && object.Key == lease.ObjectKey && object.VersionID != "" && object.VersionID != "null" && object.SHA256 != "" && object.SHA256 != *lease.ExpectedSHA {
		payload, digest, config, configHash, documentErr := completionDocuments(*lease, object.SHA256)
		if documentErr != nil {
			return false, documentErr
		}
		finish, done := context.WithTimeout(ctx, 5*time.Second)
		ok, failErr := p.Leases.Fail(finish, *lease, object, payload, digest, config, configHash)
		done()
		if failErr != nil || !ok {
			if failErr == nil {
				failErr = ErrCompletionLeaseLost
			}
			return false, failErr
		}
		committed = true
		return true, nil
	}
	if completeErr != nil {
		return false, completeErr
	}
	payload, digest, config, configHash, err := completionDocuments(*lease, object.SHA256)
	if err != nil {
		return false, err
	}
	finish, done := context.WithTimeout(ctx, 5*time.Second)
	ok, err := p.Leases.Finish(finish, *lease, object, payload, digest, config, configHash)
	done()
	if err != nil || !ok {
		if err == nil {
			err = ErrCompletionLeaseLost
		}
		return false, err
	}
	committed = true
	return true, nil
}
