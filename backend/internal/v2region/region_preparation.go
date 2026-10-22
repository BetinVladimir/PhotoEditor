package v2region

import (
	"context"
 "encoding/json"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2draft"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
)

// SQLRegionPreparation reads exact authorized metadata under transaction-local
// tenant scope. All S3 reads/canonical source/recipe proofs run after commit;
// no pool connection, snapshot transaction or SQL lock spans object IO.
type SQLRegionPreparation struct {
	API     *pgxpool.Pool
	Objects RegionObjectReader
	Bucket  string
}

func (s SQLRegionPreparation) Prepare(ctx context.Context, tenant, asset, actor, session uuid.UUID, req RegionPreviewRequest) (PreparedRegion, error) {
	if err := ctx.Err(); err != nil {
		return PreparedRegion{}, err
	}
	if s.API == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || asset == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return PreparedRegion{}, ErrRegionInvalid
	}
	var source, recipe RegionSourceMetadata
	err := database.TenantTransaction(ctx, s.API, tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return ErrRegionInvalid
		}
		var err error
		source, recipe, err = (Service{DB: tx, Objects: s.Objects, Bucket: s.Bucket}).preparationMetadata(ctx, tenant, asset, actor, session, req)
		return err
	})
	if err != nil {
		return PreparedRegion{}, err
	}
	prepared,err:=(Service{Objects: s.Objects, Bucket: s.Bucket}).finishPreparation(ctx, tenant, asset, req, source, recipe)
 if err!=nil{return PreparedRegion{},err}
 who:=auth.Identity{UserID:actor,SessionID:session}
 styleRef:=prepared.Evidence.Recipe.BaseStyle
 styleObject,err:=(v2draft.SQLArtifactCatalog{DB:s.API,Bucket:s.Bucket,ProjectID:prepared.ProjectID,AssetVersionID:req.VersionID}).Lookup(ctx,who,tenant,v2draft.LayerRef{Ref:styleRef,Contract:"StyleV2"})
 if err!=nil{return PreparedRegion{},err}
 styleBody,err:=readRegionDocument(ctx,s.Objects,styleObject,styleRef);if err!=nil{return PreparedRegion{},err}
 var style struct{Geometry struct{Enabled bool `json:"enabled"`} `json:"geometry"`;Extensions struct{Composition struct{Enabled bool `json:"enabled"`} `json:"composition"`;PortraitDetail struct{Enabled bool `json:"enabled"`} `json:"portrait_detail"`;Background struct{Mode string `json:"mode"`} `json:"background"`;Series struct{Enabled bool `json:"enabled"`} `json:"series"`;Regions []json.RawMessage `json:"region_adjustments"`;Restoration struct{Upscale string `json:"upscale"`} `json:"restoration"`} `json:"extensions"`}
 if json.Unmarshal(styleBody,&style)!=nil{return PreparedRegion{},ErrRegionInvalid}
 requires:=style.Geometry.Enabled||style.Extensions.Composition.Enabled||style.Extensions.PortraitDetail.Enabled||style.Extensions.Series.Enabled||len(style.Extensions.Regions)>0||(style.Extensions.Background.Mode!=""&&style.Extensions.Background.Mode!="keep")||(style.Extensions.Restoration.Upscale!=""&&style.Extensions.Restoration.Upscale!="off")
 var repairStyle struct{Defects struct{Enabled bool `json:"enabled"`} `json:"defects"`;Extensions struct{Restoration struct{SensorDust string `json:"sensor_dust"`} `json:"restoration"`;Inpainting struct{Enabled bool `json:"enabled"`} `json:"inpainting"`} `json:"extensions"`}
 if json.Unmarshal(styleBody,&repairStyle)!=nil{return PreparedRegion{},ErrRegionInvalid}
 requires=requires||repairStyle.Defects.Enabled||repairStyle.Extensions.Inpainting.Enabled||(repairStyle.Extensions.Restoration.SensorDust!=""&&repairStyle.Extensions.Restoration.SensorDust!="off")
 var generativeStyle struct{Extensions struct{Generation struct{Enabled bool `json:"enabled"`} `json:"generation"`;Culling struct{Enabled bool `json:"enabled"`} `json:"culling"`;Masking json.RawMessage `json:"masking"`} `json:"extensions"`}
 if json.Unmarshal(styleBody,&generativeStyle)!=nil{return PreparedRegion{},ErrRegionInvalid}
 requires=requires||generativeStyle.Extensions.Generation.Enabled||generativeStyle.Extensions.Culling.Enabled
 requires=requires||(len(generativeStyle.Extensions.Masking)>0&&string(generativeStyle.Extensions.Masking)!="null")
 if !requires{return prepared,nil}
 catalog:=regionExecutionCatalog{API:s.API,Objects:s.Objects,Bucket:s.Bucket,Tenant:tenant,Asset:asset,Version:req.VersionID,Actor:actor,Session:session,Source:source.Ref,Style:styleRef}
 candidates,err:=catalog.candidates(ctx);if err!=nil{return PreparedRegion{},err}
 // Never guess newest among different accepted geometry executions.
 if len(candidates)!=1{return PreparedRegion{},ErrRegionUnavailable}
 records,err:=catalog.records(ctx,candidates[0]);if err!=nil{return PreparedRegion{},err}
 execution,err:=compileRegionExecution(ctx,source.Ref,candidates[0].Target,styleRef,prepared.Evidence.Source,records);if err!=nil{return PreparedRegion{},err}
 if len(candidates[0].CullingAuthority)>0&&string(candidates[0].CullingAuthority)!="null"{execution.CullingAuthority=candidates[0].CullingAuthority}
 inputs,err:=catalog.executionInputs(ctx,candidates[0],execution,prepared.Plan.InputRequirements());if err!=nil{return PreparedRegion{},err}
 prepared.Plan,err=prepared.Plan.freezeExecution(execution,inputs);if err!=nil{return PreparedRegion{},err}
 return prepared,nil
}

var _ RegionPreparation = SQLRegionPreparation{}
