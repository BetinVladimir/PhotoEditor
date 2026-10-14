package media

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Signer interface {
	SignRead(context.Context, objectstore.Object, time.Duration) (objectstore.ReadURL, error)
}

type Service struct {
	DB         *pgxpool.Pool
	Objects    Signer
	Bucket     string
	SRGBSHA256 string
}

type Request struct {
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	Variants       []string  `json:"variants"`
}

type Link struct {
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	ArtifactSHA256 string    `json:"artifact_sha256"`
	ICCSHA256      string    `json:"icc_sha256"`
	ProfileHash    string    `json:"profile_hash"`
	Variant        string    `json:"variant"`
	URL            string    `json:"url"`
	ExpiresAt      time.Time `json:"expires_at"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	MediaType      string    `json:"media_type"`
}

type Links struct {
	Items []Link `json:"items"`
}

var profiles = map[string]struct {
	Edge, Quality int
}{"thumb320": {320, 82}, "thumb640": {640, 82}, "preview1600": {1600, 88}, "preview2560": {2560, 88}}

func ProfileHash(variant, srgbSHA string) (string, error) {
	profile, ok := profiles[variant]
	if _, err := domain.ParseDigest(srgbSHA); !ok || err != nil {
		return "", errors.New("unknown media variant")
	}
	return canonical.Hash(struct {
		SchemaVersion int    `json:"schema_version"`
		Variant       string `json:"variant"`
		LongEdge      int    `json:"long_edge"`
		Quality       int    `json:"quality"`
		ICC           string `json:"icc_sha256"`
		NoUpscale     bool   `json:"no_upscale"`
		Metadata      string `json:"metadata_policy"`
	}{1, variant, profile.Edge, profile.Quality, srgbSHA, true, "stripped"})
}

func normalize(request Request, srgbSHA string) (Request, map[string]string, error) {
	if request.AssetVersionID == uuid.Nil || len(request.Variants) < 1 || len(request.Variants) > 4 {
		return request, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	hashes, seen := map[string]string{}, map[string]bool{}
	for _, variant := range request.Variants {
		if seen[variant] {
			return request, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		seen[variant] = true
		hash, err := ProfileHash(variant, srgbSHA)
		if err != nil {
			return request, nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		hashes[variant] = hash
	}
	return request, hashes, nil
}

func (s Service) Sign(ctx context.Context, who auth.Identity, tenantID, assetID uuid.UUID, raw Request) (Links, error) {
	out := Links{Items: []Link{}}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return out, errors.New("media service dependencies unavailable")
	}
	request, hashes, err := normalize(raw, s.SRGBSHA256)
	if err != nil {
		return out, err
	}
	type resolved struct {
		variant       string
		object        objectstore.Object
		width, height int
	}
	resolvedItems := make([]resolved, 0, len(request.Variants))
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT a.project_id FROM photo.assets a JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.asset_id=a.id
			WHERE a.tenant_id=$1 AND a.id=$2 AND v.id=$3 AND a.deleted_at IS NULL AND a.upload_state='verified'`, tenantID, assetID, request.AssetVersionID).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); err != nil {
			return err
		}
		for _, variant := range request.Variants {
			var item resolved
			item.variant = variant
			err = tx.QueryRow(ctx, `SELECT ar.object_key,ar.object_version,ar.sha256,ar.bytes,ar.media_type,ar.width,ar.height
				FROM photo.derivatives d JOIN photo.artifacts ar ON ar.tenant_id=d.tenant_id AND ar.id=d.artifact_id
				WHERE d.tenant_id=$1 AND d.asset_version_id=$2 AND d.variant=$3 AND d.profile_hash=$4 AND d.status='ready'
				AND ar.bucket=$5 AND ar.state='ready' AND ar.object_version IS NOT NULL AND ar.object_version NOT IN ('','null')
				AND ar.bytes>0 AND ar.width>0 AND ar.height>0 AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())`,
				tenantID, request.AssetVersionID, variant, hashes[variant], s.Bucket).Scan(&item.object.Key, &item.object.VersionID, &item.object.SHA256,
				&item.object.Bytes, &item.object.ContentType, &item.width, &item.height)
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("MEDIA_NOT_READY", 409)
			}
			if err != nil {
				return err
			}
			resolvedItems = append(resolvedItems, item)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	for _, item := range resolvedItems {
		link, signErr := s.Objects.SignRead(ctx, item.object, 10*time.Minute)
		if signErr != nil {
			return Links{}, signErr
		}
		if !validMediaSignedRead(item.object, link, time.Now()) {
			return Links{}, domain.NewError("MEDIA_SIGNED_LINK_INVALID", 503)
		}
		out.Items = append(out.Items, Link{AssetVersionID: request.AssetVersionID, ArtifactSHA256: item.object.SHA256, ICCSHA256: s.SRGBSHA256, ProfileHash: hashes[item.variant], Variant: item.variant, URL: link.URL, ExpiresAt: link.ExpiresAt, Width: item.width, Height: item.height, MediaType: item.object.ContentType})
	}
	return out, nil
}

func validMediaSignedRead(object objectstore.Object, link objectstore.ReadURL, now time.Time) bool {
	return objectstore.ValidSignedReadURL(link.URL, object.VersionID) && link.ExpiresAt.After(now) && !link.ExpiresAt.After(now.Add(10*time.Minute+5*time.Second))
}
