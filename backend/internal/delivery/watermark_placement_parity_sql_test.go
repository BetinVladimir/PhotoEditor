package delivery
import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)
func TestIntegrationWatermarkPlacementSQLGoParity(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated privileged database migrated through01768 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest([]byte("image"))};mark:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest([]byte("mark"))}
 for _,anchor:=range []string{"top_left","top_right","bottom_left","bottom_right","center","invalid"}{
  for _,geometry:=range [][6]int{{10,8,10,10,0,0},{10,2,10,10,0,0},{10,8,10,10,9,0},{10,8,10,10,0,7},{100,80,2,3,1,1}}{
   preset:=exportpreset.Watermark{AssetID:uuid.New(),Opacity:.5,Anchor:anchor,MarginX:geometry[4],MarginY:geometry[5]}
   _,_,goErr:=CompileDeliveryWatermarkParams(image,mark,geometry[0],geometry[1],geometry[2],geometry[3],preset)
   body,err:=canonical.Marshal(preset);if err!=nil{t.Fatal(err)}
   tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)}
   if _,err=tx.Exec(ctx,"SET LOCAL ROLE photo_fn_owner");err!=nil{_ =tx.Rollback(ctx);t.Fatal(err)}
   var accepted bool
   err=tx.QueryRow(ctx,`SELECT photo.v30_watermark_placement_ready($1,$2,$3,$4,$5::jsonb)`,geometry[0],geometry[1],geometry[2],geometry[3],string(body)).Scan(&accepted)
   _ =tx.Rollback(ctx)
   if err!=nil||accepted!=(goErr==nil){t.Fatal("placement parity",anchor,geometry,accepted,goErr,err)}
  }
 }
}

