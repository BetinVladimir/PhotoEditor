package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

type retouchFaceCatalog struct{ Source, Landmarks PlanSource }

// SQLRetouchPreviewFaceResolver reads one unambiguous canonical landmark catalog.
// Every face in a collection is decoded. Competing catalogs require regeneration
// or an explicit future selection policy; no arbitrary latest-result fallback.
func SQLRetouchPreviewFaceResolver(db *pgxpool.Pool, read PlanObjectReader) (RetouchPreviewFaceResolver, error) {
	if db == nil || read == nil {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, who auth.Identity, p RetouchPreviewParams) ([]RetouchPreviewFace, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
			return nil, ErrInvalid
		}
		if _, _, err := FreezeRetouchPreviewParams(p); err != nil {
			return nil, err
		}
		sourceBody, err := canonical.Marshal(p.Source)
		if err != nil {
			return nil, err
		}
		load := func() (retouchFaceCatalog, error) {
			empty := retouchFaceCatalog{}
			tx, err := db.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				return empty, err
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", p.TenantID.String()); err != nil {
				return empty, err
			}
			var found retouchFaceCatalog
			var contract string
			err = tx.QueryRow(ctx, "SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_approved_image_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", p.TenantID, p.IntentID, who.UserID, who.SessionID, p.WorkflowRevision, p.TargetID, p.TargetRevision, p.Source.AssetVersionID, p.Candidate.ArtifactID, p.Candidate.SHA256, false, p.Source.Source.ArtifactID, p.Source.Source.SHA256).Scan(&contract, &found.Source.Ref.ArtifactID, &found.Source.Ref.SHA256, &found.Source.Bytes, &found.Source.Bucket, &found.Source.Key, &found.Source.Version, &found.Source.MediaType)
			if err != nil {
				return empty, err
			}
			if contract != "WorkingImage" || found.Source.Ref != p.Source.Source {
				return empty, ErrInvalid
			}
			rows, err := tx.Query(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_retouch_preview_landmark_catalog($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", p.TenantID, p.IntentID, p.TargetID, who.UserID, who.SessionID, p.WorkflowRevision, p.TargetRevision, p.Plan.SHA256, p.Candidate.SHA256, string(sourceBody))
			if err != nil {
				return empty, err
			}
			count := 0
			for rows.Next() {
				count++
				if count > 1 {
					rows.Close()
					return empty, ErrConflict
				}
				err = rows.Scan(&found.Landmarks.Ref.ArtifactID, &found.Landmarks.Ref.SHA256, &found.Landmarks.Bucket, &found.Landmarks.Key, &found.Landmarks.Version, &found.Landmarks.Bytes, &found.Landmarks.MediaType)
				if err != nil {
					rows.Close()
					return empty, err
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return empty, err
			}
			if count != 1 {
				return empty, ErrUnavailable
			}
			for _, s := range []PlanSource{found.Source, found.Landmarks} {
				if s.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(s.Ref.SHA256) || s.Bytes < 2 || s.Bytes > 1048576 || s.MediaType != "application/json" || s.Key == "" || s.Bucket == "" || s.Version == "" || s.Version == "null" {
					return empty, ErrInvalid
				}
			}
			if found.Landmarks.Ref.ArtifactID == found.Source.Ref.ArtifactID {
				return empty, ErrInvalid
			}
			if err = tx.Commit(ctx); err != nil {
				return empty, err
			}
			return found, nil
		}
		original, err := load()
		if err != nil {
			return nil, err
		}
		readExact := func(s PlanSource) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			raw, err := read(ctx, s)
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			sum := sha256.Sum256(raw)
			if int64(len(raw)) != s.Bytes || hex.EncodeToString(sum[:]) != s.Ref.SHA256 {
				return nil, ErrInvalid
			}
			return append([]byte(nil), raw...), nil
		}
		source, err := readExact(original.Source)
		if err != nil {
			return nil, err
		}
		landmarks, err := readExact(original.Landmarks)
		if err != nil {
			return nil, err
		}
		faces, err := DecodeRetouchPreviewFaces(p, source, original.Landmarks.Ref, landmarks)
		if err != nil {
			return nil, err
		}
		latest, err := load()
		if err != nil {
			return nil, err
		}
		if latest != original {
			return nil, ErrConflict
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return faces, nil
	}, nil
}
