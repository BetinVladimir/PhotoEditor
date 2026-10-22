package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

// Final freeze publication retains the original build as storage owner until
// the separately qualified compose graph is atomically admitted. It cannot
// release or reinterpret the original Apply reservation.
type editorFinalDocuments struct{DB *pgxpool.Pool;Scope artifactjson.Scope;Bucket,SHA string;Bytes int64}
func(r editorFinalDocuments)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
	if err:=ctx.Err();err!=nil{return artifactjson.Reservation{},err}
	if r.DB==nil||scope!=r.Scope||sha!=r.SHA||size!=r.Bytes{return artifactjson.Reservation{},objectstore.ErrInvalid}
	var id,token uuid.UUID;var ready bool
	err:=r.DB.QueryRow(ctx,`SELECT * FROM photo.reserve_editor_effect_final_recipe($1,$2,$3,$4)`,scope.Tenant,scope.Run,scope.Owner,r.Bucket).Scan(&id,&token,&ready)
	if errors.Is(err,pgx.ErrNoRows){return artifactjson.Reservation{},artifactjson.ErrStale};if err!=nil{return artifactjson.Reservation{},err}
	if err:=ctx.Err();err!=nil{return artifactjson.Reservation{},err}
	if id==uuid.Nil||token==uuid.Nil{return artifactjson.Reservation{},objectstore.ErrIntegrity}
	result:=artifactjson.Reservation{ArtifactID:id,Token:token};if ready{result.Ready=&artifactjson.Ref{ID:id,SHA256:sha}};return result,nil
}
func(r editorFinalDocuments)Complete(ctx context.Context,scope artifactjson.Scope,res artifactjson.Reservation,object objectstore.Object)(artifactjson.Ref,error){
	if err:=ctx.Err();err!=nil{return artifactjson.Ref{},err};if r.DB==nil{return artifactjson.Ref{},objectstore.ErrInvalid}
	key,err:=artifactjson.DocumentKey(scope,res.ArtifactID)
	if err!=nil||scope!=r.Scope||object.Key!=key||object.SHA256!=r.SHA||object.Bytes!=r.Bytes||object.ContentType!="application/json"||object.VersionID==""||object.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrIntegrity}
	var accepted bool;err=r.DB.QueryRow(ctx,`SELECT photo.complete_editor_effect_final_recipe($1,$2,$3,$4,$5,$6,$7,$8,$9)`,scope.Tenant,scope.Run,scope.Owner,res.ArtifactID,res.Token,object.SHA256,object.Bytes,r.Bucket,object.VersionID).Scan(&accepted)
	if err!=nil{return artifactjson.Ref{},err};if err:=ctx.Err();err!=nil{return artifactjson.Ref{},err};if !accepted{return artifactjson.Ref{},artifactjson.ErrStale};return artifactjson.Ref{ID:res.ArtifactID,SHA256:r.SHA},nil
}
var _ artifactjson.Repository=editorFinalDocuments{}
