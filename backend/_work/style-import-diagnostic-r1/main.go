// Exact idempotent retry of the already-authorized real Style.Import request.
// Never print raw SQL/errors, credentials, tokens, parameters or exception locals.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/style"
	"time"
)

type intent struct {
	Tenant  string `json:"tenant_id"`
	Project string `json:"project_id"`
	Actor   string `json:"actor_id"`
	Key     string `json:"import_key"`
	SHA     string `json:"package_sha256"`
}
type completion struct {
	Artifact string `json:"upload_artifact_id"`
	State    string `json:"state"`
	SHA      string `json:"sha256"`
}

func file(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return errors.New("PRIVATE_INPUT_READ_FAILED")
	}
	if len(b) > 1<<20 {
		return errors.New("INPUT_TOO_LARGE")
	}
	return json.Unmarshal(b, v)
}
func pool(ctx context.Context, key, role string) (*pgxpool.Pool, error) {
	c, e := pgxpool.ParseConfig(os.Getenv(key))
	if e != nil {
		return nil, errors.New("DATABASE_CONFIG_INVALID")
	}
	c.MaxConns = 2
	c.ConnConfig.RuntimeParams["role"] = role
	c.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	c.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	database.GuardApplicationRole(c)
	p, e := pgxpool.NewWithConfig(ctx, c)
	if e != nil {
		return nil, errors.New("POOL_FAILED")
	}
	if e = database.RequireApplicationRole(ctx, p, role); e != nil {
		p.Close()
		return nil, errors.New("APPLICATION_ROLE_REFUSED")
	}
	return p, nil
}
func run() map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result := map[string]any{"status": "DIAGNOSTIC_SETUP_REFUSED", "worker_mutations": false}
	var i intent
	var c completion
	var session struct {
		Access string `json:"access_token"`
		User   struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if file(os.Getenv("INTENT_FILE"), &i) != nil || file(os.Getenv("COMPLETION_FILE"), &c) != nil || file(os.Getenv("SESSION_FILE"), &session) != nil {
		return result
	}
	if i.Tenant != "5dbe7f17-b560-4696-9525-03959e843841" || i.Project != "3f5e6a21-40ad-4b06-a9d9-370c021ba814" || i.Actor != "db3f69a8-525b-467b-bacd-fd9dbf1514b5" || i.Key != "e0882e00-eb4d-434d-ad79-86a4bdbf9f74" || i.SHA != "6577feb6630ffdae94f7958b80a0856651ef8bc831a021ab8c5a2f3f41bcddf7" || session.User.ID != i.Actor || c.State != "ready" || c.SHA != i.SHA || c.Artifact != "e80fc273-531f-48a9-a98d-9a3023765de7" {
		return result
	}
	api, e := pool(ctx, "API_DATABASE_URL", "photo_api")
	if e != nil {
		return result
	}
	defer api.Close()
	authDB, e := pool(ctx, "AUTH_DATABASE_URL", "photo_auth")
	if e != nil {
		return result
	}
	defer authDB.Close()
	signer, _, _, e := auth.LoadKeys(os.Getenv("AUTH_KEYS_FILE"), "photoeditor", "photo-api")
	if e != nil {
		return result
	}
	who, e := (&auth.Service{DB: authDB, Signer: signer}).AuthenticateAccess(ctx, session.Access)
	if e != nil || who.UserID.String() != i.Actor {
		result["status"] = "EXACT_CURRENT_AUTH_REFUSED"
		return result
	}
	response, e := (style.Service{DB: api, Bucket: os.Getenv("S3_BUCKET")}).Import(ctx, who, uuid.MustParse(i.Tenant), i.Key, style.ImportRequest{ProjectID: uuid.MustParse(i.Project), Name: "Manual warm color — test actor pending review", UploadArtifactID: uuid.MustParse(c.Artifact)})
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) {
			return map[string]any{"status": "EXACT_REAL_IMPORT_PG_ERROR", "pgcode": pg.Code, "schema": pg.SchemaName, "table": pg.TableName, "column": pg.ColumnName, "constraint": pg.ConstraintName, "routine": pg.Routine}
		}
		return map[string]any{"status": "EXACT_REAL_IMPORT_NON_PG_ERROR", "error_class": fmt.Sprintf("%T", e)}
	}
	return map[string]any{"status": "EXACT_REAL_IMPORT_IDEMPOTENT_SUCCESS", "http_status": response.Status, "response": json.RawMessage(response.Body)}
}
func main() { json.NewEncoder(os.Stdout).Encode(run()) }
