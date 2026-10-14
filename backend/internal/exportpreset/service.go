package exportpreset

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	generated "photoeditor/backend/internal/db/generated"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/tenant"
)

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
}
type Size struct {
	LongEdge     *int   `json:"long_edge,omitempty"`
	Width        *int   `json:"width,omitempty"`
	Height       *int   `json:"height,omitempty"`
	Fit          string `json:"fit"`
	AllowUpscale bool   `json:"allow_upscale"`
}
type Watermark struct {
	AssetID uuid.UUID `json:"asset_id"`
	Opacity float64   `json:"opacity"`
	Anchor  string    `json:"anchor"`
	MarginX int       `json:"margin_x"`
	MarginY int       `json:"margin_y"`
}
type Sharpen struct {
	Radius    float64 `json:"radius"`
	Strength  float64 `json:"strength"`
	Threshold float64 `json:"threshold"`
}
type Config struct {
	Format         string     `json:"format"`
	ICCID          string     `json:"icc_id"`
	Size           *Size      `json:"size,omitempty"`
	Quality        *int       `json:"quality,omitempty"`
	Subsampling    string     `json:"subsampling"`
	Alpha          string     `json:"alpha"`
	MatteRGB       []float64  `json:"matte_rgb,omitempty"`
	MetadataPolicy string     `json:"metadata_policy"`
	Watermark      *Watermark `json:"watermark,omitempty"`
	Sharpen        *Sharpen   `json:"sharpen,omitempty"`
	NamingTemplate string     `json:"naming_template"`
}
type Create struct {
	Name   string `json:"name"`
	Config Config `json:"config"`
}
type Preset struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Revision   string    `json:"revision"`
	Config     Config    `json:"config"`
	ConfigHash string    `json:"config_hash"`
	CreatedAt  time.Time `json:"created_at"`
}

func validate(req Create) error {
	if strings.TrimSpace(req.Name) == "" || len([]rune(req.Name)) > 120 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return ValidateConfig(req.Config)
}

// ValidateConfig is shared by mutable preset authoring and frozen delivery admission.
func ValidateConfig(c Config) error {
	if _, err := canonical.Marshal(c); err != nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if !map[string]bool{"tiff": true, "png": true, "jpeg": true}[c.Format] || !map[string]bool{"srgb_v1": true, "work_rec2020_gamma24_v1": true}[c.ICCID] || c.Subsampling != "444" || !map[string]bool{"preserve": true, "matte": true}[c.Alpha] || !map[string]bool{"strip": true, "copyright": true, "approved_exif": true}[c.MetadataPolicy] || strings.TrimSpace(c.NamingTemplate) == "" || len(c.NamingTemplate) > 200 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	dims := 0
	if c.Size != nil {
		if c.Size.Fit != "contain" {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		for _, v := range []*int{c.Size.LongEdge, c.Size.Width, c.Size.Height} {
			if v != nil {
				if *v < 1 || *v > 100000 {
					return domain.NewError("VALIDATION_FAILED", 422)
				}
				dims++
			}
		}
	}
	if c.Size != nil && !((dims == 1 && c.Size.LongEdge != nil) || (dims == 2 && c.Size.Width != nil && c.Size.Height != nil)) ||
		(c.Quality != nil && (*c.Quality < 1 || *c.Quality > 100)) ||
		(c.Format == "jpeg" && (c.Alpha == "preserve" || c.Quality == nil)) || (c.Format != "jpeg" && c.Quality != nil) {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if c.Alpha == "matte" {
		if len(c.MatteRGB) != 3 {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		for _, v := range c.MatteRGB {
			if v < 0 || v > 1 {
				return domain.NewError("VALIDATION_FAILED", 422)
			}
		}
	} else if len(c.MatteRGB) > 0 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if c.Watermark != nil && (c.Watermark.AssetID == uuid.Nil || c.Watermark.Opacity < 0 || c.Watermark.Opacity > 1 || !map[string]bool{"top_left": true, "top_right": true, "bottom_left": true, "bottom_right": true, "center": true}[c.Watermark.Anchor] || c.Watermark.MarginX < 0 || c.Watermark.MarginY < 0) {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if c.Sharpen != nil && (c.Sharpen.Radius < .1 || c.Sharpen.Radius > 5 || c.Sharpen.Strength < 0 || c.Sharpen.Strength > 1 || c.Sharpen.Threshold < 0 || c.Sharpen.Threshold > .1) {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return nil
}
func fromListRow(row generated.ListExportPresetsRow) (Preset, error) {
	var config Config
	if err := json.Unmarshal(row.Config, &config); err != nil {
		return Preset{}, err
	}
	return Preset{ID: row.ID, Name: row.Name, Revision: strconv.FormatInt(row.Revision, 10), Config: config, ConfigHash: row.ConfigHash, CreatedAt: row.CreatedAt.Time}, nil
}
func fromCreateRow(row generated.CreateExportPresetRow) (Preset, error) {
	var config Config
	if err := json.Unmarshal(row.Config, &config); err != nil {
		return Preset{}, err
	}
	return Preset{ID: row.ID, Name: row.Name, Revision: strconv.FormatInt(row.Revision, 10), Config: config, ConfigHash: row.ConfigHash, CreatedAt: row.CreatedAt.Time}, nil
}

func (s Service) List(ctx context.Context, who auth.Identity, tid uuid.UUID, limit int, raw string) (pagination.Page[Preset], error) {
	out := pagination.Page[Preset]{Items: []Preset{}}
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), Sort: "export_preset:created_at,id"}
	var afterID *uuid.UUID
	var afterTime *time.Time
	if raw != "" {
		c, e := s.Cursors.Decode(raw, scope)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, e := uuid.Parse(c.LastID)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		t, e := time.Parse(time.RFC3339Nano, c.LastPosition)
		if e != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		afterID = &id
		afterTime = &t
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var cursorTime pgtype.Timestamptz
		if afterTime != nil {
			cursorTime = pgtype.Timestamptz{Time: *afterTime, Valid: true}
		}
		rows, e := generated.New(tx).ListExportPresets(ctx, generated.ListExportPresetsParams{TenantID: tid, AfterTime: cursorTime, AfterID: afterID, RowLimit: int32(limit + 1)})
		if e != nil {
			return e
		}
		for _, row := range rows {
			p, e := fromListRow(row)
			if e != nil {
				return e
			}
			out.Items = append(out.Items, p)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		scope.LastID = last.ID.String()
		scope.LastPosition = last.CreatedAt.Format(time.RFC3339Nano)
		next, e := s.Cursors.Encode(scope)
		if e != nil {
			return out, e
		}
		out.NextCursor = &next
	}
	return out, nil
}
func (s Service) Create(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req Create) (idempotency.Response, error) {
	var out idempotency.Response
	if err := validate(req); err != nil {
		return out, err
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "ExportPreset.Create", Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			if req.Config.Watermark != nil {
				var ok bool
				e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.assets WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND upload_state='verified')`, tid, req.Config.Watermark.AssetID).Scan(&ok)
				if e != nil {
					return idempotency.Response{}, e
				}
				if !ok {
					return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
				}
			}
			configJSON, e := canonical.Marshal(req.Config)
			if e != nil {
				return idempotency.Response{}, e
			}
			h, e := canonical.Hash(req.Config)
			if e != nil {
				return idempotency.Response{}, e
			}
			row, e := generated.New(tx).CreateExportPreset(ctx, generated.CreateExportPresetParams{TenantID: tid, Name: strings.TrimSpace(req.Name), Config: configJSON, ConfigHash: h})
			if e != nil {
				return idempotency.Response{}, e
			}
			p, e := fromCreateRow(row)
			if e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(p)
			return idempotency.Response{Status: 201, Body: body, ResourceID: &p.ID}, e
		})
		return e
	})
	return out, err
}
