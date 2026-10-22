package interchange

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type SQLPreparedImportArtifactStore struct {
	DB       *pgxpool.Pool
	Tenant   uuid.UUID
	Identity ImportDocumentIdentity
	Parsed   []byte
	Bucket   string
}

func (s SQLPreparedImportArtifactStore) receipt(scope ImportPreparedScope, ref v2input.Ref, size int64, media string) ([]byte, []byte, error) {
	if s.DB == nil || s.Tenant == uuid.Nil || scope.Tenant != s.Tenant || scope.Run != s.Identity.Run || scope.Workflow != s.Identity.Workflow || scope.Run == uuid.Nil || scope.Workflow == uuid.Nil || scope.Entry == uuid.Nil || scope.Binding.AssetVersionID == uuid.Nil || !validImportRef(scope.Binding.Source) || !validImportRef(v2input.Ref{ArtifactID: scope.Entry, SHA256: scope.Binding.CanvasHash}) || !validImportRef(ref) || size < 1 || size > 128<<30 || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return nil, nil, v2input.ErrInvalid
	}
	// Snapshot the optional certificate before validation and receipt serialization.
	if scope.Warp != nil {
		owned := *scope.Warp
		scope.Warp = &owned
	}
	if scope.Background != nil {
		owned := *scope.Background
		scope.Background = &owned
	}
	if scope.LUT != nil {
		owned := *scope.LUT
		scope.LUT = &owned
	}
	if scope.DefectLabel != nil {
		owned := *scope.DefectLabel
		scope.DefectLabel = &owned
	}
 if scope.Historical!=nil{owned:=*scope.Historical;if owned.OriginalBoxes!=nil{boxes:=*owned.OriginalBoxes;owned.OriginalBoxes=&boxes};scope.Historical=&owned}
 if scope.DefectPNG!=nil{owned:=*scope.DefectPNG;owned.Reports=append([]v2input.Ref(nil),owned.Reports...);scope.DefectPNG=&owned}
 if err:=validateImportDefectPNGOccurrence(scope,ref);err!=nil{return nil,nil,err}
	if err := validateImportBackgroundOccurrence(scope, ref); err != nil {
		return nil, nil, err
	}
	if err := validateImportWarpOccurrence(scope, ref); err != nil {
		return nil, nil, err
	}
	if err := validateImportLUTOccurrence(scope, ref); err != nil {
		return nil, nil, err
	}
	if err := validateImportDefectLabelOccurrence(scope, ref); err != nil {
		return nil, nil, err
	}
	if len(s.Parsed) < 2 || len(s.Parsed) > 8<<20 {
		return nil, nil, v2input.ErrInvalid
	}
	parsed := append([]byte(nil), s.Parsed...)
	normalized, err := canonical.Bytes(parsed)
	if err != nil || !bytes.Equal(parsed, normalized) {
		return nil, nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(context.Background(), parsed); err != nil {
		return nil, nil, err
	}
	switch scope.Contract {
 case "DefectLabelPNG":
  if media!="image/png"||size<8||size>512<<20{return nil,nil,v2input.ErrInvalid}
	case "StyleBackgroundAsset":
		if size < 8 || size > 536870912 || (media != "image/jpeg" && media != "image/png" && media != "image/tiff") {
			return nil, nil, v2input.ErrInvalid
		}
	case "MaskPixels", "DenseWarpField", "AbsoluteLUTArray", "DefectLabelMap", "WorkingPixels", "WorkingAlpha":
		if media != "application/octet-stream" {
			return nil, nil, v2input.ErrInvalid
		}
	case "EditingRecipeV3", "StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef", "Params", "Stats20", "ResolvedStyle", "DefectCandidates", "DefectReview", "DustCandidates", "DefectProposeReport", "WorkingImage", "FaceLandmarks", "FaceBoxes", "HistoricalOriginReceipt":
		if media != "application/json" || size < 2 || size > 8<<20 {
			return nil, nil, v2input.ErrInvalid
		}
	default:
		return nil, nil, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(struct {
		Entry       uuid.UUID                     `json:"entry_id"`
		Binding     v2input.SessionVersionBinding `json:"binding"`
		SHA         string                        `json:"sha256"`
		Bytes       int64                         `json:"bytes"`
		Media       string                        `json:"media_type"`
		Warp        *ImportWarpOccurrence         `json:"warp_occurrence,omitempty"`
		LUT         *ImportLUTOccurrence          `json:"lut_occurrence,omitempty"`
		DefectLabel *ImportDefectLabelOccurrence  `json:"defect_label_occurrence,omitempty"`
 Historical *HistoricalOriginPayload `json:"historical_origin_occurrence,omitempty"`
 DefectPNG *ImportDefectPNGOccurrence `json:"defect_png_occurrence,omitempty"`
		Background  *ImportBackgroundSource       `json:"background_occurrence,omitempty"`
	}{scope.Entry, scope.Binding, ref.SHA256, size, media, scope.Warp, scope.LUT, scope.DefectLabel, scope.Historical, scope.DefectPNG, scope.Background})
	return raw, parsed, err
}
func scanPreparedImportReservation(row pgx.Row, ref v2input.Ref) (artifactjson.Reservation, error) {
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	if err := row.Scan(&outcome, &id, &token, &ready); err != nil {
		return artifactjson.Reservation{}, err
	}
	switch outcome {
	case "quota":
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	case "conflict":
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	case "created", "resumed", "replayed":
	default:
		return artifactjson.Reservation{}, artifactjson.ErrStale
	}
	if id == nil || *id != ref.ArtifactID || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: ref.SHA256}
	}
	return result, nil
}
func (s SQLPreparedImportArtifactStore) ReservePreparedImport(ctx context.Context, scope ImportPreparedScope, ref v2input.Ref, size int64, media string) (artifactjson.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Reservation{}, err
	}
	receipt, parsed, err := s.receipt(scope, ref, size, media)
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	var result artifactjson.Reservation
	err = database.TenantTransaction(ctx, s.DB, s.Tenant, func(tx pgx.Tx) error {
		if err := (SQLImportDocumentStore{Tenant: s.Tenant}).checkRun(ctx, tx, s.Identity); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_interchange_prepared($1,$2,$3,$4,$5,$6,$7)`, s.Tenant, scope.Workflow, string(parsed), ref.ArtifactID, scope.Contract, string(receipt), s.Bucket)
		var err error
		result, err = scanPreparedImportReservation(row, ref)
		return err
	})
	return result, err
}
func (s SQLPreparedImportArtifactStore) CompletePreparedImport(ctx context.Context, scope ImportPreparedScope, ref v2input.Ref, reservation artifactjson.Reservation, object objectstore.Object) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	receipt, parsed, err := s.receipt(scope, ref, object.Bytes, object.ContentType)
	if err != nil {
		return v2input.Ref{}, err
	}
	key, err := objectstore.DocumentKey(s.Tenant, scope.Run, ref.ArtifactID)
	if err != nil {
		return v2input.Ref{}, err
	}
	if reservation.ArtifactID != ref.ArtifactID || reservation.Token == uuid.Nil || object.Key != key || object.SHA256 != ref.SHA256 || object.VersionID == "" || object.VersionID == "null" {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	err = database.TenantTransaction(ctx, s.DB, s.Tenant, func(tx pgx.Tx) error {
		if err := (SQLImportDocumentStore{Tenant: s.Tenant}).checkRun(ctx, tx, s.Identity); err != nil {
			return err
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v30_interchange_prepared($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tenant, scope.Workflow, string(parsed), ref.ArtifactID, scope.Contract, string(receipt), s.Bucket, reservation.Token, object.VersionID).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
	if err != nil {
		return v2input.Ref{}, err
	}
	return ref, ctx.Err()
}
