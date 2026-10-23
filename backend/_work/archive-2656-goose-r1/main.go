package main
import("context";"crypto/sha256";"database/sql";"encoding/hex";"flag";"fmt";"os";"strings";"testing/fstest";"time";"github.com/jackc/pgx/v5";_ "github.com/jackc/pgx/v5/stdlib";"github.com/pressly/goose/v3")
func main(){
 file:=flag.String("sql","","canonical SQL");pin:=flag.String("sha256","","SQL pin");dsnfile:=flag.String("dsn-file","","private owned cluster DSN");flag.Parse()
 raw,e:=os.ReadFile(*file);if e!=nil{panic("SQL unavailable")};h:=sha256.Sum256(raw);if hex.EncodeToString(h[:])!=*pin{panic("SQL drift")}
 secret,e:=os.ReadFile(*dsnfile);if e!=nil{panic("DSN unavailable")};cfg,e:=pgx.ParseConfig(string(secret));if e!=nil{panic("DSN invalid")}
 if cfg.Host!="127.0.0.1"||cfg.Port==5432||cfg.Port==56432||cfg.Port==57432||!strings.HasPrefix(cfg.Database,"archive_timeout"){panic("not isolated PG")}
 db,e:=sql.Open("pgx",string(secret));if e!=nil{panic("open")};defer db.Close();ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 var head int64;if e=db.QueryRowContext(ctx,"SELECT max(version_id)FROM public.goose_db_version WHERE is_applied").Scan(&head);e!=nil||head!=2655{panic("head2655 required")}
 if goose.SetDialect("postgres")!=nil{panic("dialect")};goose.SetBaseFS(fstest.MapFS{"02656_archive_materializer_timeout_and_alias.sql":{Data:raw}})
 if e=goose.UpToContext(ctx,db,".",2656);e!=nil{panic("Goose2656 failed")}
 if e=db.QueryRowContext(ctx,"SELECT max(version_id)FROM public.goose_db_version WHERE is_applied").Scan(&head);e!=nil||head!=2656{panic("head2656 required")}
 fmt.Println("ACTUAL_ISOLATED_NORMAL_GOOSE_UPTO_2656_PASS")
}
