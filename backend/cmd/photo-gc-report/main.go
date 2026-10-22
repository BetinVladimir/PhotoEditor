package main

import (
 "context"
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "io"
 "os"
 "os/signal"
 "syscall"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/maintenance"
)

type options struct { tenant,after string; limit int }
func parseOptions(args []string) (options,error) {
 var out options
 flags:=flag.NewFlagSet("photo-gc-report",flag.ContinueOnError)
 flags.SetOutput(io.Discard)
 flags.StringVar(&out.tenant,"tenant","","tenant UUID (required)")
 flags.StringVar(&out.after,"after","","artifact UUID cursor")
 flags.IntVar(&out.limit,"limit",100,"maximum artifacts (1..100)")
 if err:=flags.Parse(args);err!=nil{return options{},errors.New("invalid report arguments")}
 tenant,err:=uuid.Parse(out.tenant)
 if err!=nil||tenant==uuid.Nil||flags.NArg()!=0||out.limit<1||out.limit>100{return options{},errors.New("usage: photo-gc-report --tenant UUID [--after UUID] [--limit 1..100]")}
 out.tenant=tenant.String()
 if out.after!="" {cursor,err:=uuid.Parse(out.after);if err!=nil{return options{},errors.New("invalid artifact cursor")};out.after=cursor.String()}
 return out,nil
}

// This command accepts an operator credential only. It neither grants membership
// nor offers a runtime API; PostgreSQL must authorize selection of fn_owner.
func run(ctx context.Context,args []string,dsn string,output io.Writer) error {
 options,err:=parseOptions(args);if err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 if dsn=="" {return errors.New("PHOTO_GC_REPORT_OPERATOR_DATABASE_URL required")}
 config,err:=pgxpool.ParseConfig(dsn);if err!=nil{return errors.New("invalid operator database configuration")}
 config.MaxConns=1;config.MinConns=0
 config.ConnConfig.ConnectTimeout=3*time.Second
 config.ConnConfig.RuntimeParams["role"]="photo_fn_owner"
 config.ConnConfig.RuntimeParams["default_transaction_read_only"]="on"
 config.ConnConfig.RuntimeParams["statement_timeout"]="4000"
 config.ConnConfig.RuntimeParams["lock_timeout"]="500"
 bounded,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 pool,err:=pgxpool.NewWithConfig(bounded,config);if err!=nil{return reportStageError(bounded,err,"operator database unavailable")};defer pool.Close()
 // Schema checks use only catalog reads. No cleaner or repair is invoked.
 if err=maintenance.RequireArtifactGCAccounting(bounded,pool);err!=nil{return reportStageError(bounded,err,"installed GC accounting contract unavailable")}
 report,err:=maintenance.ReadHistoricalGCReport(bounded,pool,options.tenant,options.after,options.limit)
 if err!=nil{return reportStageError(bounded,err,"historical GC report unavailable; no report emitted")}
 if err=bounded.Err();err!=nil{return err}
 return json.NewEncoder(output).Encode(report)
}
// Preserve current cancellation without exposing database or credential details.
func reportStageError(ctx context.Context, err error, message string) error {
 if current:=ctx.Err();current!=nil{return current}
 if errors.Is(err,context.Canceled){return context.Canceled}
 if errors.Is(err,context.DeadlineExceeded){return context.DeadlineExceeded}
 return errors.New(message)
}
func main(){
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop()
 if err:=run(ctx,os.Args[1:],os.Getenv("PHOTO_GC_REPORT_OPERATOR_DATABASE_URL"),os.Stdout);err!=nil{fmt.Fprintln(os.Stderr,"photo-gc-report:",err);os.Exit(1)}
}
