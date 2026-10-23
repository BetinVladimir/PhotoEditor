// MAIN only, external root-reviewed producer window is mandatory.
package main
import("context";"crypto/sha256";"database/sql";"encoding/hex";"errors";"fmt";"os";"strings";"testing/fstest";"time";"github.com/jackc/pgx/v5";"github.com/jackc/pgx/v5/pgconn";_ "github.com/jackc/pgx/v5/stdlib";"github.com/pressly/goose/v3")
const root="/Users/freelancer/Documents/PhotoEditor/"
func hash(b []byte)string{v:=sha256.Sum256(b);return hex.EncodeToString(v[:])}
func read(p,h string)([]byte,error){b,e:=os.ReadFile(root+p);if e!=nil||hash(b)!=h{return nil,errors.New("pinned source drift")};return b,nil}
func run()error{
 if os.Getenv("CONFIRM_ARCHIVE_2656_MAIN")!="reviewed-main2655-to2656"{return errors.New("explicit MAIN confirmation required")}
 cfg,e:=pgx.ParseConfig(os.Getenv("DATABASE_URL"));if e!=nil||cfg.Host!="127.0.0.1"||cfg.Port!=57432||cfg.Database!="photoeditor"||cfg.User!="rtx_snapshot_admin"{return errors.New("exact MAIN target required")}
 raw,e:=read("backend/db/migrations/02656_archive_materializer_timeout_and_alias.sql","551c3cbcf1520de804b57587c9be9e86bcf3a2275fb84def7d65ce9c0e6ab866");if e!=nil{return e}
 before,e:=read("work/archive-main-2656-r1/guards-before.sql","4613887d06a623734b2f583f80a6d2e8bf77f8644f2c325cbd86934b7fed004f");if e!=nil{return e};after,e:=read("work/archive-main-2656-r1/guards-after.sql","961827db7d5bca13d749735346abaa60729ecfb9182264efd23aad7b1b883e68");if e!=nil{return e}
 up:="-- +goose Up\n";down:="-- +goose Down";text:=string(raw)
 if strings.Count(text,up)!=1||strings.Count(text,down)!=1||strings.Contains(text,"NO TRANSACTION"){return errors.New("transaction anchors drift")}
 derived:=strings.Replace(text,up,up+"-- +goose StatementBegin\n"+string(before)+"-- +goose StatementEnd\n",1)
 derived=strings.Replace(derived,down,"-- +goose StatementBegin\n"+string(after)+"-- +goose StatementEnd\n"+down,1)
 db,e:=sql.Open("pgx",os.Getenv("DATABASE_URL"));if e!=nil{return errors.New("database unavailable")};defer db.Close();db.SetMaxOpenConns(1)
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second);defer cancel();var head int64
 if e=db.QueryRowContext(ctx,"SELECT max(version_id)FROM goose_db_version WHERE is_applied").Scan(&head);e!=nil||head!=2655{return errors.New("head2655 required")}
 goose.SetBaseFS(fstest.MapFS{"02656_archive_materializer_timeout_and_alias.sql":{Data:[]byte(derived)}});if e=goose.SetDialect("postgres");e!=nil{return e}
 if e=goose.UpToContext(ctx,db,".",2656);e!=nil{return e}
 if e=db.QueryRowContext(ctx,"SELECT max(version_id)FROM goose_db_version WHERE is_applied").Scan(&head);e!=nil||head!=2656{return errors.New("posthead2656 required")}
 fmt.Println("MAIN_GOOSE2656_COMMITTED_CATALOG_USERDATA_GUARDS_PASS");return nil
}
func main(){if e:=run();e!=nil{var p *pgconn.PgError;if errors.As(e,&p){fmt.Fprintf(os.Stderr,"SQLSTATE=%s\n",p.Code)};fmt.Fprintf(os.Stderr,"FAILED_CLASS=%T\n",e);os.Exit(1)}}
