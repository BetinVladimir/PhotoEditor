package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"syscall"
)

// A canonical, hash-pinned private operator file is a policy selector, never
// qualification itself. Prepare still reads and verifies the real SQL policy
// and physical report/calibration at every current admission boundary.
type AuthoredNativeDeployment struct {
	SchemaVersion int    `json:"schema_version"`
	ContractsSHA  string `json:"contracts_sha"`
	Upscale       *struct {
		Expected v2qualification.Expected `json:"expected"`
		Report   v2input.Ref              `json:"report"`
	} `json:"upscale"`
	Panorama *AuthoredNativeMultiDeployment `json:"panorama"`
	Focus    *AuthoredNativeMultiDeployment `json:"focus"`
}

func ReadAuthoredNativeDeployment(ctx context.Context, path, expectedSHA string) (AuthoredNativeDeployment, error) {
	var zero AuthoredNativeDeployment
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if path == "" || len(expectedSHA) != 64 {
		return zero, v2input.ErrInvalid
	}
	info, e := os.Lstat(path)
	if e != nil {
		return zero, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 2 || info.Size() > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	file, e := openAuthoredNativeDeploymentFile(path, info)
	if e != nil {
		return zero, e
	}
	defer file.Close()
	opened, e := file.Stat()
	if e != nil {
		return zero, e
	}
	if !os.SameFile(info, opened) {
		return zero, v2input.ErrExpired
	}
	raw, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if e != nil {
		return zero, e
	}
	after, e := file.Stat()
	if e != nil {
		return zero, e
	}
	current, e := os.Lstat(path)
	if e != nil {
		return zero, e
	}
	if !sameAuthoredNativeDeploymentFile(opened, after) || !sameAuthoredNativeDeploymentFile(opened, current) || int64(len(raw)) != opened.Size() || len(raw) > 1<<20 {
		return zero, v2input.ErrExpired
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != expectedSHA {
		return zero, v2input.ErrInvalid
	}
	canonicalRaw, e := canonical.Bytes(raw)
	if e != nil || !bytes.Equal(raw, canonicalRaw) {
		return zero, v2input.ErrInvalid
	}
	var d AuthoredNativeDeployment
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || decoder.Decode(new(any)) != io.EOF {
		return zero, v2input.ErrInvalid
	}
	if d.SchemaVersion != 1 || d.ContractsSHA != AuthoredGeometrySchemasSHA || (d.Upscale == nil && d.Panorama == nil && d.Focus == nil) {
		return zero, v2input.ErrInvalid
	}
	if d.Upscale != nil {
		owned, _, e := freezeGeometryNativeExpected(d.Upscale.Expected)
		if e != nil {
			return zero, e
		}
		d.Upscale.Expected = owned
		if !validLayerArtifactRef(d.Upscale.Report) {
			return zero, v2input.ErrInvalid
		}
	}
	for operation, value := range map[string]*AuthoredNativeMultiDeployment{"optical.panorama": d.Panorama, "optical.focus_stack": d.Focus} {
		if value == nil {
			continue
		}
		if value.Locks.Operation != operation {
			return zero, v2input.ErrInvalid
		}
		if _, e := value.Locks.Expected(); e != nil {
			return zero, e
		}
		if _, e := value.Pin.Compile(); e != nil {
			return zero, e
		}
	}
	return d, ctx.Err()
}
func NewSQLAuthoredNativePreparation(ctx context.Context, scheduler, api *pgxpool.Pool, objects v2input.SessionAnalysisObjectReader, publisher artifactjson.Objects, bucket, path, sha string) (*SQLAuthoredNativePreparation, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if scheduler == nil || api == nil || objects == nil || publisher == nil || bucket == "" {
		return nil, renderUnavailable()
	}
	d, e := ReadAuthoredNativeDeployment(ctx, path, sha)
	if e != nil {
		return nil, e
	}
	p := &SQLAuthoredNativePreparation{Scheduler: scheduler, API: api, Objects: objects, PublisherObjects: publisher, Bucket: bucket, Panorama: d.Panorama, Focus: d.Focus}
	if d.Upscale != nil {
		p.UpscaleExpected = d.Upscale.Expected
		p.UpscaleReport = d.Upscale.Report
	}
	return p, nil
}

// Same policy as the existing repository native export deployment loader.
// Access time is intentionally excluded: ordinary reads can update it.
func sameAuthoredNativeDeploymentFile(before, current os.FileInfo) bool {
	return before != nil && current != nil && current.Mode().IsRegular() && current.Mode().Perm()&0077 == 0 && current.Size() >= 2 && current.Size() <= 1<<20 && os.SameFile(before, current) && current.Size() == before.Size() && current.Mode() == before.Mode() && current.ModTime().Equal(before.ModTime())
}

// This boundary is separately exercised after an actual Lstat and substitution.
// It accepts no opener callback, trust result, or caller-supplied deployment bytes.
func openAuthoredNativeDeploymentFile(path string, before os.FileInfo) (*os.File, error) {
	file, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	opened, e := file.Stat()
	if e != nil {
		file.Close()
		return nil, e
	}
	if !sameAuthoredNativeDeploymentFile(before, opened) {
		file.Close()
		return nil, v2input.ErrExpired
	}
	return file, nil
}
