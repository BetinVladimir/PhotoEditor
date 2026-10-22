package v2render

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"time"
)

type LensPreviewArrayCatalog interface {
	ResolveArray(context.Context, LensPreviewSource, v2draft.LayerRef, []byte) (objectstore.Object, error)
}
type SQLLensPreviewArrays struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s SQLLensPreviewArrays) ResolveArray(ctx context.Context, pin LensPreviewSource, node v2draft.LayerRef, sourceRaw []byte) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := validateLensPreviewSource(pin); err != nil {
		return zero, err
	}
	if node.Contract != "WorkingPixels" && node.Contract != "WorkingAlpha" {
		return zero, v2input.ErrInvalid
	}
	if node.Path != "$/source/pixels" && node.Path != "$/source/alpha" {
		return (SQLLensPreviewObjects{DB: s.DB, Bucket: s.Bucket}).ResolveObject(ctx, pin, node)
	}
	if s.DB == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || len(sourceRaw) < 2 || len(sourceRaw) > 1048576 || node.ParentRef != pin.Base.Source || !validLayerArtifactRef(node.Ref) || (node.Path == "$/source/pixels") != (node.Contract == "WorkingPixels") {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", pin.Tenant.String()); err != nil {
		return zero, err
	}
	var bucket string
	var obj objectstore.Object
	err = tx.QueryRow(ctx, "SELECT * FROM photo.v25_lens_preview_original_array($1,$2,$3,$4,$5,$6,$7,$8,$9)", pin.Tenant, pin.Producer, pin.ResultID, pin.Attempt, pin.Fence, string(sourceRaw), node.Ref.ArtifactID, node.Ref.SHA256, node.Contract).Scan(&bucket, &obj.Key, &obj.VersionID, &obj.Bytes, &obj.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	obj.SHA256 = node.Ref.SHA256
	if bucket != s.Bucket || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.Bytes < 1 || (obj.ContentType != "application/x-npy" && obj.ContentType != "application/octet-stream") {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return obj, nil
}

type LensPreviewArrayWitness struct {
	Node   v2draft.LayerRef
	Object objectstore.Object
}
type VerifiedLensPreviewInputs struct {
	source LensPreviewSource
	arrays []LensPreviewArrayWitness
}

func (v VerifiedLensPreviewInputs) Source() LensPreviewSource { return v.source }
func (v VerifiedLensPreviewInputs) Arrays() []LensPreviewArrayWitness {
	return append([]LensPreviewArrayWitness(nil), v.arrays...)
}

// VerifyLensPreviewInputs streams RGB and alpha only. Dense-warp arrays
// are metadata ancestry, not sampled by media.preview, which displays this canvas.
func VerifyLensPreviewInputs(ctx context.Context, sources LensPreviewSourceReader, catalog LensPreviewArrayCatalog, objects v2input.SessionAnalysisObjectReader, bundle LensPreviewBundle) (VerifiedLensPreviewInputs, error) {
	var zero VerifiedLensPreviewInputs
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if sources == nil || catalog == nil || objects == nil {
		return zero, v2input.ErrInvalid
	}
	pin := bundle.pin
	if err := validateLensPreviewSource(pin); err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(bundle.documents[bundle.result.WorkingImage.ArtifactID.String()])
	if err != nil || image.DType != "float32" {
		return zero, v2input.ErrInvalid
	}
	parent := bundle.result.WorkingImage
	prefix := "$/working_image"
	if parent == pin.Base.Source {
		prefix = "$/source"
	}
	nodes := []v2draft.LayerRef{{Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, ParentRef: parent, Contract: "WorkingPixels", Path: prefix + "/pixels"}}
	if image.Alpha != nil {
		nodes = append(nodes, v2draft.LayerRef{Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, ParentRef: parent, Contract: "WorkingAlpha", Path: prefix + "/alpha"})
	}
	seen := map[string]bool{}
	witnesses := make([]LensPreviewArrayWitness, 0, len(nodes))
	for _, node := range nodes {
		if seen[node.Ref.ArtifactID.String()] || node.Ref.ArtifactID == parent.ArtifactID || node.Ref.ArtifactID == pin.Result.ArtifactID {
			return zero, v2input.ErrInvalid
		}
		seen[node.Ref.ArtifactID.String()] = true
		obj, err := catalog.ResolveArray(ctx, pin, node, bundle.documents[pin.Base.Source.ArtifactID.String()])
		if err != nil {
			return zero, err
		}
		channels := int64(3)
		if node.Contract == "WorkingAlpha" {
			channels = 1
		}
		if obj.Bytes < 1 || obj.Bytes > image.Canvas.Width*image.Canvas.Height*channels*4+65536 || obj.SHA256 != node.Ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || (obj.ContentType != "application/x-npy" && obj.ContentType != "application/octet-stream") {
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
			err = masknpy.VerifyAlphaNPY(ctx, reader, obj.Bytes, obj.SHA256, image.Canvas.Width, image.Canvas.Height)
		} else {
			err = masknpy.VerifyFloat32ArrayNPY(ctx, reader, obj.Bytes, obj.SHA256, image.Canvas.Width, image.Canvas.Height, 3, false)
		}
		closeErr := reader.Close()
		if err != nil {
			return zero, err
		}
		if closeErr != nil {
			return zero, closeErr
		}
		current, err := catalog.ResolveArray(ctx, pin, node, bundle.documents[pin.Base.Source.ArtifactID.String()])
		if err != nil {
			return zero, err
		}
		if current != obj {
			return zero, v2input.ErrExpired
		}
		witnesses = append(witnesses, LensPreviewArrayWitness{Node: node, Object: obj})
	}
	current, err := sources.Resolve(ctx, pin.Tenant, pin.Producer)
	if err != nil {
		return zero, err
	}
	if current != pin {
		return zero, v2input.ErrExpired
	}
	return VerifiedLensPreviewInputs{source: pin, arrays: witnesses}, nil
}
