package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PythonMetadataSourcePreparer struct {
	Python, WorkerRoot string
	Timeout            time.Duration
	Environment        map[string]string
}

func rawMetadataOriginalMIME(media string) bool {
	switch media {
	case "image/x-adobe-dng", "image/x-canon-cr2", "image/x-nikon-nef", "image/x-sony-arw",
		"image/x-fuji-raf", "image/x-olympus-orf", "image/x-panasonic-rw2":
		return true
	default:
		return false
	}
}

var metadataSourceEnvironmentKeys = []string{"DATABASE_URL", "S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "APP_ENV",
	"PHOTO_METADATA_SOURCE_SCRATCH_ROOT", "PHOTO_METADATA_SOURCE_MAX_BYTES", "PHOTO_METADATA_SOURCE_RAM_BYTES", "PHOTO_METADATA_SOURCE_SCRATCH_BYTES",
	"PHOTO_METADATA_SOURCE_EXIFTOOL", "PHOTO_METADATA_SOURCE_EXIFTOOL_SHA256"}

func (p PythonMetadataSourcePreparer) environment() ([]string, error) {
	if !filepath.IsAbs(p.Python) || filepath.Clean(p.Python) != p.Python || !filepath.IsAbs(p.WorkerRoot) || filepath.Clean(p.WorkerRoot) != p.WorkerRoot ||
		p.Timeout < time.Second || p.Timeout > 10*time.Minute || p.Timeout%time.Second != 0 {
		return nil, v2input.ErrInvalid
	}
	allowed := map[string]bool{}
	for _, key := range metadataSourceEnvironmentKeys {
		allowed[key] = true
		if key != "APP_ENV" && key != "PHOTO_METADATA_SOURCE_EXIFTOOL" && key != "PHOTO_METADATA_SOURCE_EXIFTOOL_SHA256" && p.Environment[key] == "" {
			return nil, v2input.ErrInvalid
		}
	}
	keys := make([]string, 0, len(p.Environment))
	for key, value := range p.Environment {
		if !allowed[key] || strings.ContainsRune(value, 0) {
			return nil, v2input.ErrInvalid
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	root := p.Environment["PHOTO_METADATA_SOURCE_SCRATCH_ROOT"]
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || len(p.Environment["S3_BUCKET"]) < 3 || len(p.Environment["S3_BUCKET"]) > 63 {
		return nil, v2input.ErrInvalid
	}
	if raw := p.Environment["PHOTO_METADATA_SOURCE_EXIFTOOL"]; raw != "" && (!filepath.IsAbs(raw) || filepath.Clean(raw) != raw) {
		return nil, v2input.ErrInvalid
	}
	rawTool, pin := p.Environment["PHOTO_METADATA_SOURCE_EXIFTOOL"], p.Environment["PHOTO_METADATA_SOURCE_EXIFTOOL_SHA256"]
	if (rawTool == "") != (pin == "") || pin != "" && !shaPattern.MatchString(pin) {
		return nil, v2input.ErrInvalid
	}
	integer := func(key string, maximum int64) (int64, error) {
		raw := p.Environment[key]
		if len(raw) == 0 || len(raw) > 16 {
			return 0, v2input.ErrInvalid
		}
		for _, c := range raw {
			if c < '0' || c > '9' {
				return 0, v2input.ErrInvalid
			}
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 || value > maximum {
			return 0, v2input.ErrInvalid
		}
		return value, nil
	}
	size, err := integer("PHOTO_METADATA_SOURCE_MAX_BYTES", 2<<30)
	if err != nil {
		return nil, err
	}
	ram, err := integer("PHOTO_METADATA_SOURCE_RAM_BYTES", 9007199254740991)
	if err != nil {
		return nil, err
	}
	scratch, err := integer("PHOTO_METADATA_SOURCE_SCRATCH_BYTES", 9007199254740991)
	if err != nil {
		return nil, err
	}
	if ram < (256<<20)+size*4 || scratch < size+(8<<20) {
		return nil, v2input.ErrInvalid
	}
	environment := []string{"PATH=/usr/bin:/bin", "PYTHONPATH=" + p.WorkerRoot, "PYTHONDONTWRITEBYTECODE=1", "PYTHONNOUSERSITE=1", "PYTHONHASHSEED=0", "OMP_NUM_THREADS=1", "OPENBLAS_NUM_THREADS=1"}
	for _, key := range keys {
		environment = append(environment, key+"="+p.Environment[key])
	}
	return environment, nil
}

// A JSON string can expand each publication byte to a six-byte escape.
const metadataSourceResponseLimit = 6*1048576 + 1024

type metadataSourceOutput struct {
	body   []byte
	cancel context.CancelFunc
}

func (o *metadataSourceOutput) Write(raw []byte) (int, error) {
	if len(raw) > metadataSourceResponseLimit-len(o.body) {
		o.cancel()
		return 0, io.ErrShortBuffer
	}
	o.body = append(o.body, raw...)
	return len(raw), nil
}
func restoreMetadataSourceResponse(raw []byte, scope MetadataSourcePublicationScope, id uuid.UUID, policy string, approved []string) (MetadataSourcePublication, error) {
	var zero MetadataSourcePublication
	var response struct {
		SchemaVersion     int    `json:"schema_version"`
		PublicationJCS    string `json:"publication_jcs"`
		PublicationSHA256 string `json:"publication_sha256"`
	}
	if len(raw) > metadataSourceResponseLimit || json.Unmarshal(raw, &response) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(response)
	if err != nil || !bytes.Equal(raw, exact) || response.SchemaVersion != 1 || len(response.PublicationJCS) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	var envelope metadataSourcePublicationEnvelope
	if json.Unmarshal([]byte(response.PublicationJCS), &envelope) != nil || envelope.MetadataSource.ArtifactID != id {
		return zero, v2input.ErrInvalid
	}
	return RestoreMetadataSourcePublication([]byte(response.PublicationJCS), response.PublicationSHA256, scope, envelope.MetadataSource, []byte(envelope.MetadataJCS), policy, approved)
}

// Prepare returns an externally rebound publication, never a database receipt.
// Its explicit environment must carry API-role credentials. Python checks the
// effective role and rechecks original authority before and after extraction.
func (p PythonMetadataSourcePreparer) Prepare(ctx context.Context, scope MetadataSourcePublicationScope, id uuid.UUID, policy string, approved []string) (MetadataSourcePublication, error) {
	var zero MetadataSourcePublication
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	environment, err := p.environment()
	if err != nil {
		return zero, err
	}
	fields := append([]string{}, approved...)
	sort.Strings(fields)
	empty, err := canonical.Marshal(MetadataDocument{})
	if err != nil {
		return zero, err
	}
	if _, err = CompileMetadataSourcePublication(scope, v2input.Ref{ArtifactID: id, SHA256: digest(empty)}, empty, policy, fields); err != nil {
		return zero, err
	}
	if rawMetadataOriginalMIME(scope.OriginalMIME) &&
		(p.Environment["PHOTO_METADATA_SOURCE_EXIFTOOL"] == "" || p.Environment["PHOTO_METADATA_SOURCE_EXIFTOOL_SHA256"] == "") {
		return zero, v2input.ErrInvalid
	}
	maximum, err := strconv.ParseInt(p.Environment["PHOTO_METADATA_SOURCE_MAX_BYTES"], 10, 64)
	if err != nil || scope.OriginalBytes > maximum {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "scope": scope, "metadata_id": id, "policy": policy, "approved_fields": fields, "timeout_seconds": int(p.Timeout / time.Second)})
	if err != nil || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	// The deadline includes queue wait; do not start a process after expiry.
	release, err := acquireMetadataSourceSlot(bounded, metadataSourceProcessSlots)
	if err != nil {
		return zero, err
	}
	defer release()
	output := &metadataSourceOutput{cancel: cancel}
	command := exec.CommandContext(bounded, p.Python, "-m", "photo_worker.v2.delivery_metadata_source_job")
	command.Dir = p.WorkerRoot
	command.Env = environment
	command.Stdin = bytes.NewReader(raw)
	command.Stdout = output
	command.Stderr = io.Discard
	command.WaitDelay = 2 * time.Second
	if err = command.Run(); err != nil {
		if bounded.Err() != nil {
			return zero, bounded.Err()
		}
		return zero, err
	}
	if err = bounded.Err(); err != nil {
		return zero, err
	}
	return restoreMetadataSourceResponse(output.body, scope, id, policy, fields)
}
