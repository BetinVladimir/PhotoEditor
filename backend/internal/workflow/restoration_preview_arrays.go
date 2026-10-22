package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type RestorationPreviewArrayCatalog interface {
	ResolveArray(context.Context, RestorationPreviewSource, string, string,
		[]byte, v2input.Ref) (objectstore.Object, error)
}

type SQLRestorationPreviewArrays struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s SQLRestorationPreviewArrays) ResolveArray(ctx context.Context,
	pin RestorationPreviewSource, side, channel string, image []byte,
	ref v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	imageRef, err := restorationPreviewSideRef(pin, side)
	if err != nil || (channel != "pixels" && channel != "alpha") ||
		ref.ArtifactID == uuid.Nil || domainDigestInvalid(ref.SHA256) ||
		len(image) < 2 || len(image) > 1048576 ||
		renderDerivativeBytesSHA(image) != imageRef.SHA256 || s.DB == nil ||
		len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	err = withPerspectivePreviewDocumentTx(ctx, s.DB, pin.Tenant,
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				"SELECT photo.v28_restoration_preview_array($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)",
				pin.Tenant, pin.Producer, pin.Review, pin.ResultID,
				pin.Result.ArtifactID, pin.Result.SHA256, side, channel,
				string(image), ref.ArtifactID, ref.SHA256, s.Bucket).Scan(&raw)
		})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	obj, err := parseRestorationPreviewArrayLocator(raw, ref)
	if err != nil {
		return zero, err
	}
	return obj, nil
}

func parseRestorationPreviewArrayLocator(raw []byte, ref v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if len(raw) < 2 || len(raw) > 4096 {
		return zero, v2input.ErrExpired
	}
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire struct {
		Key     string `json:"key"`
		Version string `json:"version"`
		SHA     string `json:"sha256"`
		Bytes   int64  `json:"bytes"`
		Media   string `json:"media_type"`
	}
	if json.Unmarshal(canonicalRaw, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(exact, canonicalRaw) || wire.SHA != ref.SHA256 ||
		wire.Key == "" || len(wire.Key) > 1024 || wire.Version == "" ||
		wire.Version == "null" || len(wire.Version) > 1024 || wire.Bytes < 14 ||
		wire.Bytes > 2400065536 ||
		(wire.Media != "application/x-npy" && wire.Media != "application/octet-stream") {
		return zero, v2input.ErrInvalid
	}
	return objectstore.Object{Key: wire.Key, VersionID: wire.Version,
		SHA256: wire.SHA, Bytes: wire.Bytes, ContentType: wire.Media}, nil
}

type RestorationPreviewArrayWitness struct {
	Side, Channel string
	Ref           v2input.Ref
	Object        objectstore.Object
}

type VerifiedRestorationPreviewInputs struct {
	source RestorationPreviewSource
	arrays []RestorationPreviewArrayWitness
}

func (v VerifiedRestorationPreviewInputs) Source() RestorationPreviewSource { return v.source }
func (v VerifiedRestorationPreviewInputs) Arrays() []RestorationPreviewArrayWitness {
	return append([]RestorationPreviewArrayWitness(nil), v.arrays...)
}

func (v VerifiedRestorationPreviewInputs) AdmissionJSON() ([]byte, error) {
	if validateRestorationPreviewSource(v.source) != nil ||
		(len(v.arrays) != 2 && len(v.arrays) != 4) {
		return nil, v2input.ErrInvalid
	}
	values := make([]map[string]any, len(v.arrays))
	for i, item := range v.arrays {
		wantSide := "before"
		if i >= len(v.arrays)/2 {
			wantSide = "after"
		}
		wantChannel := "pixels"
		if len(v.arrays) == 4 && i%2 == 1 {
			wantChannel = "alpha"
		}
		if item.Side != wantSide || item.Channel != wantChannel ||
			item.Ref.ArtifactID == uuid.Nil || domainDigestInvalid(item.Ref.SHA256) ||
			item.Object.SHA256 != item.Ref.SHA256 || item.Object.Bytes < 14 ||
			item.Object.Key == "" || item.Object.VersionID == "" ||
			item.Object.VersionID == "null" {
			return nil, v2input.ErrInvalid
		}
		values[i] = map[string]any{"side": item.Side, "channel": item.Channel,
			"ref": item.Ref, "object_key": item.Object.Key,
			"object_version": item.Object.VersionID,
			"sha256":         item.Object.SHA256, "bytes": item.Object.Bytes,
			"media_type": item.Object.ContentType}
	}
	return canonical.Marshal(values)
}

type RestorationPreviewSourceReader interface {
	Resolve(context.Context, uuid.UUID, uuid.UUID) (RestorationPreviewSource, error)
}

// VerifyRestorationPreviewInputs streams every NPY consumed by the four tasks.
func VerifyRestorationPreviewInputs(ctx context.Context, sources RestorationPreviewSourceReader,
	catalog RestorationPreviewArrayCatalog, objects RestorationPreviewImageObjects,
	plan RestorationPreviewPlan, baseRaw, candidateRaw []byte) (VerifiedRestorationPreviewInputs, error) {
	var zero VerifiedRestorationPreviewInputs
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if sources == nil || catalog == nil || objects == nil ||
		validateRestorationPreviewIntent(plan) != nil {
		return zero, v2input.ErrInvalid
	}
	rebuilt, err := CompileRestorationPreviewPlan(plan.source, plan.srgb, baseRaw, candidateRaw,plan.policy)
	if err != nil || rebuilt.run != plan.run {
		return zero, v2input.ErrInvalid
	}
	bodies := [2][]byte{baseRaw, candidateRaw}
	witnesses := make([]RestorationPreviewArrayWitness, 0, 4)
	for sideIndex, side := range []string{"before", "after"} {
		image, err := review.ParseWorkingImage(bodies[sideIndex])
		if err != nil {
			return zero, err
		}
		entries := []struct {
			channel string
			ref     review.ArtifactRef
		}{{"pixels", image.Pixels}}
		if image.Alpha != nil {
			entries = append(entries, struct {
				channel string
				ref     review.ArtifactRef
			}{"alpha", *image.Alpha})
		}
		for _, entry := range entries {
			ref := v2input.Ref{ArtifactID: entry.ref.ID, SHA256: entry.ref.SHA256}
			obj, err := catalog.ResolveArray(ctx, plan.source, side, entry.channel,
				bodies[sideIndex], ref)
			if err != nil {
				return zero, err
			}
			channels := int64(3)
			if entry.channel == "alpha" {
				channels = 1
			}
			if obj.SHA256 != ref.SHA256 || obj.Key == "" || obj.VersionID == "" ||
				obj.VersionID == "null" || obj.Bytes < 14 ||
				obj.Bytes > image.Canvas.Width*image.Canvas.Height*channels*4+65536 ||
				(obj.ContentType != "application/x-npy" && obj.ContentType != "application/octet-stream") {
				return zero, v2input.ErrInvalid
			}
			reader, err := objects.Open(ctx, obj)
			if err != nil {
				if reader != nil {
					_ = reader.Close()
				}
				return zero, err
			}
			if reader == nil {
				return zero, v2input.ErrInvalid
			}
			if channels == 1 {
				err = masknpy.VerifyAlphaNPY(ctx, reader, obj.Bytes, obj.SHA256,
					image.Canvas.Width, image.Canvas.Height)
			} else {
				err = masknpy.VerifyFloat32ArrayNPY(ctx, reader, obj.Bytes, obj.SHA256,
					image.Canvas.Width, image.Canvas.Height, 3, false)
			}
			closeErr := reader.Close()
			if err != nil {
				return zero, err
			}
			if closeErr != nil {
				return zero, closeErr
			}
			current, err := catalog.ResolveArray(ctx, plan.source, side, entry.channel,
				bodies[sideIndex], ref)
			if err != nil {
				return zero, err
			}
			if current != obj {
				return zero, v2input.ErrExpired
			}
			witnesses = append(witnesses, RestorationPreviewArrayWitness{
				Side: side, Channel: entry.channel, Ref: ref, Object: obj})
		}
	}
	current, err := sources.Resolve(ctx, plan.source.Tenant, plan.source.Producer)
	if err != nil {
		return zero, err
	}
	if current != plan.source {
		return zero, v2input.ErrExpired
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return VerifiedRestorationPreviewInputs{source: plan.source, arrays: witnesses}, nil
}
