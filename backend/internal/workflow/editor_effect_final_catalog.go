package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// The dedicated final catalog is scheduler-private. SQL proves membership of
// the exact independently adopted origin, not merely tenant artifact ownership.
// The normal accepted-source catalog remains responsible for original recipe
// traversal; this catalog cannot redefine the asset version's master image.
type editorFinalCatalog struct {
	DB *pgxpool.Pool
	Tenant, Build uuid.UUID
	Actor auth.Identity
	Bucket string
}

func(c editorFinalCatalog)Lookup(ctx context.Context,who auth.Identity,tenant uuid.UUID,node v2draft.LayerRef)(objectstore.Object,error){
	var object objectstore.Object
	if err:=ctx.Err();err!=nil{return object,err}
	if c.DB==nil||c.Tenant==uuid.Nil||c.Build==uuid.Nil||c.Bucket==""||tenant!=c.Tenant||who.UserID!=c.Actor.UserID||who.SessionID!=c.Actor.SessionID||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||node.Ref.ArtifactID==uuid.Nil{return object,v2input.ErrInvalid}
	var bucket string
	err:=c.DB.QueryRow(ctx,`SELECT * FROM photo.editor_effect_final_artifact($1,$2,$3,$4)`,tenant,c.Build,node.Ref.ArtifactID,node.Ref.SHA256).Scan(&bucket,&object.Key,&object.VersionID,&object.Bytes,&object.ContentType)
	if errors.Is(err,pgx.ErrNoRows){return object,v2input.ErrExpired};if err!=nil{return object,err};if err:=ctx.Err();err!=nil{return object,err}
	if bucket!=c.Bucket||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.Bytes<1{return object,objectstore.ErrIntegrity}
	object.SHA256=node.Ref.SHA256;return object,nil
}

var _ v2draft.ArtifactCatalog=editorFinalCatalog{}
