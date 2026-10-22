// Bounded operator composition of the actual production style pipeline.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/workflow"
	"strings"
	"time"
)

var tenant = uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841")
var run = uuid.MustParse("13dce0c7-d888-4bf1-a550-99a9e1cef6d5")
var style = uuid.MustParse("4f134bde-5bfb-42d6-a386-125f07619816")
var task = uuid.MustParse("d0700d5d-f843-43aa-b7ac-69d98c7269bf")
var pack = uuid.MustParse("e80fc273-531f-48a9-a98d-9a3023765de7")

type checkedRow struct{ pgx.Row }

func (r checkedRow) Scan(d ...any) error {
	if e := r.Row.Scan(d...); e != nil {
		return e
	}
	if len(d) != 9 || *d[0].(*uuid.UUID) != tenant || *d[1].(*uuid.UUID) != run || *d[2].(*uuid.UUID) != style || *d[3].(*uuid.UUID) != task || *d[4].(*uuid.UUID) != pack || *d[5].(**uuid.UUID) != nil {
		return errors.New("style import claimed outside exact scope")
	}
	return nil
}

type scopedDB struct{ pool *pgxpool.Pool }

func (d scopedDB) QueryRow(c context.Context, q string, a ...any) pgx.Row {
	r := d.pool.QueryRow(c, q, a...)
	if strings.Contains(q, "FROM photo.claim_style_import_build()") {
		return checkedRow{r}
	}
	return r
}

type candidateRepo struct {
	workflow.SQLStyleCandidateRepository
}

func (r candidateRepo) Claim(c context.Context) (*workflow.StyleCandidateLease, error) {
	l, e := r.SQLStyleCandidateRepository.Claim(c)
	if e != nil || l == nil {
		return l, e
	}
	if l.TenantID != tenant || l.RunID != run || l.StyleID != style {
		return nil, errors.New("candidate outside exact scope")
	}
	return l, nil
}
func (r candidateRepo) Finish(c context.Context, l workflow.StyleCandidateLease, config json.RawMessage, hash string, metrics json.RawMessage, low bool, code string) (bool, error) {
	if l.TenantID != tenant || l.RunID != run || l.StyleID != style {
		return false, errors.New("candidate finish scope")
	}
	return r.SQLStyleCandidateRepository.Finish(c, l, config, hash, metrics, low, code)
}
func execute() error {
	if os.Getenv("CONFIRM_EXACT_STYLE_CYCLE") != "13dce0c7-d888-4bf1-a550-99a9e1cef6d5" {
		return errors.New("exact confirmation required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if e != nil {
		return errors.New("database config")
	}
	cfg.MaxConns = 3
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	database.GuardApplicationRole(cfg)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return errors.New("database pool")
	}
	defer pool.Close()
	if e = database.RequireApplicationRole(ctx, pool, "photo_scheduler"); e != nil {
		return e
	}
	storageConfig := objectstore.ConfigFromEnv(true)
	store, e := objectstore.New(storageConfig)
	if e != nil {
		return e
	}
	db := scopedDB{pool}
	coordinator := workflow.StyleImportCoordinator{Store: workflow.StyleImportStore{DB: db}, Publisher: artifactjson.Publisher{Repository: workflow.StyleImportDocuments{DB: db, Bucket: storageConfig.Bucket}, Objects: store}}
	coordinated, e := coordinator.ProcessOne(ctx)
	if e != nil {
		return e
	}
	dispatched, e := (workflow.Dispatcher{DB: pool}).Dispatch(ctx, 20)
	if e != nil {
		return e
	}
	materialized, e := (workflow.StyleCandidateMaterializer{Repository: candidateRepo{workflow.SQLStyleCandidateRepository{DB: pool, Bucket: storageConfig.Bucket}}, Objects: store}).ProcessOne(ctx)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "EXACT_REAL_STYLE_COMPONENT_CYCLE", "coordinated": coordinated, "dispatch_confirmations": dispatched, "candidate_materialized": materialized, "photographer_approval": false})
}
func main() {
	if e := execute(); e != nil {
		metadata := map[string]string{"status": "STYLE_COMPONENT_FAILED", "error_class": fmt.Sprintf("%T", e)}
		var pg *pgconn.PgError
		if errors.As(e, &pg) {
			metadata["pgcode"] = pg.Code
			metadata["schema"] = pg.SchemaName
			metadata["table"] = pg.TableName
			metadata["column"] = pg.ColumnName
			metadata["constraint"] = pg.ConstraintName
			metadata["routine"] = pg.Routine
		}
		_ = json.NewEncoder(os.Stderr).Encode(metadata)
		os.Exit(1)
	}
}
