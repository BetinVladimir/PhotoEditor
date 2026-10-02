// photo-v2-qualifications is an operator-only, one-shot import command.
package main

import (
	"context"
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
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2qualification"
)

type importArgs struct {
	tenant, project, operator, report uuid.UUID
	sha, locks                        string
}

func parseImportArgs(args []string) (importArgs, error) {
	var result importArgs
	if len(args) == 0 || args[0] != "import" {
		return result, errors.New("usage: photo-v2-qualifications import --tenant UUID --project UUID --operator UUID --report-artifact UUID --sha256 SHA --locks PATH")
	}
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var tenant, project, operator, report string
	flags.StringVar(&tenant, "tenant", "", "tenant UUID")
	flags.StringVar(&project, "project", "", "project UUID")
	flags.StringVar(&operator, "operator", "", "operator UUID")
	flags.StringVar(&report, "report-artifact", "", "report artifact UUID")
	flags.StringVar(&result.sha, "sha256", "", "report SHA256")
	flags.StringVar(&result.locks, "locks", "", "trusted deployment lock file")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || result.locks == "" ||
		len(result.sha) != 64 {
		return importArgs{}, errors.New("invalid import arguments")
	}
	for _, c := range result.sha {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return importArgs{}, errors.New("invalid report SHA256")
			}
		}
	}
	var err error
	if result.tenant, err = uuid.Parse(tenant); err != nil || result.tenant == uuid.Nil {
		return importArgs{}, errors.New("invalid tenant")
	}
	if result.project, err = uuid.Parse(project); err != nil || result.project == uuid.Nil {
		return importArgs{}, errors.New("invalid project")
	}
	if result.operator, err = uuid.Parse(operator); err != nil || result.operator == uuid.Nil {
		return importArgs{}, errors.New("invalid operator")
	}
	if result.report, err = uuid.Parse(report); err != nil || result.report == uuid.Nil {
		return importArgs{}, errors.New("invalid report artifact")
	}
	return result, nil
}

func run(ctx context.Context, args []string, output io.Writer) error {
	selected, err := parseImportArgs(args)
	if err != nil {
		return err
	}
	expected, err := v2qualification.LoadLocks(selected.locks)
	if err != nil {
		return err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	dbConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	dbConfig.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	dbConfig.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	dbConfig.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	db, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		return errors.New("database unavailable")
	}
	defer db.Close()
	environment := os.Getenv("APP_ENV")
	if environment == "" {
		environment = "development"
	}
	storageConfig := objectstore.ConfigFromEnv(environment == "development")
	storage, err := objectstore.New(storageConfig)
	if err != nil {
		return errors.New("object storage configuration invalid")
	}
	limit, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	qualified, err := (v2qualification.Importer{DB: db, Objects: storage,
		Bucket: storageConfig.Bucket}).Import(limit, selected.tenant,
		selected.project, selected.report, selected.sha, selected.operator,
		expected)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, qualified.String())
	return err
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt,
		syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
