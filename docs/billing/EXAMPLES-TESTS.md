# EXAMPLES-TESTS — образцы тестов

Сгенерировано `tools/gen-examples.py`. Вручную не править.

## 1. Python integration-тест SQL-функции (PostgreSQL, миграции до нужного head)

Форма: `pytestmark=pytest.mark.migration_head(N)` фиксирует head миграций; фикстура `db` — одноразовая БД; проверяется точный повтор, конфликт по другому hash и отсутствие второго списания.

### Источник: `tests/integration/test_billing_routed_apply.py (шапка)`

```python
"""Routed admission against real canonical adoption and PostgreSQL transaction races.
Synthetic scene/run fixtures; no pixel execution or Stripe network calls.
"""
import json
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
import test_billing_apply as base
from test_billing_apply import enroll,adopt,consumed
from test_apply_adoption_atomicity import _adoptable
from test_billing_quota import grant,check_conservation,rejects,race
```

### Источник: `tests/integration/test_billing_routed_apply.py (test_routed_binding_replay_and_conflict)`

```python
def test_routed_binding_replay_and_conflict(db,route):
    t,r,k,items,out=_adoptable(db,4,admit=base.bind)
    req,actor,session,key=db.execute('SELECT id,actor_user_id,session_id,request_key FROM photo.billing_usage_requests WHERE run_id=%s',(r,)).fetchone()
    args=(t,actor,session,r,key,sha256(key.encode()).digest(),'api-test',route)
    assert db.execute('SELECT photo.billing_bind_routed_apply(%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()==(req,)
    rejects(db,'SELECT photo.billing_bind_routed_apply(%s,%s,%s,%s,%s,%s,%s,%s)',(*args[:5],sha256(b'changed').digest(),*args[6:]),'BILLING_IDEMPOTENCY_CONFLICT')
    assert db.execute('SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=%s',(t,)).fetchone()==(2,)
```

## 2. Go integration-harness под ролью photo_scheduler/photo_api

Форма: тест запускается из Python с фикстурой в env; без неё `t.Skip`. Это значит: `go test` без Python-обвязки НЕ проверяет эти сценарии — не считать их пройденными.

### Источник: `backend/internal/billing/operations_integration_test.go (начало)`

```go
)

// Invoked by Python with an isolated migrated DB and authentic actor/session.
func TestIntegrationOperationColdAdmission(t *testing.T) {
	raw := os.Getenv("PHOTO_BILLING_OPERATION_FIXTURE")
	if raw == "" {
		t.Skip("isolated operation fixture required")
	}
	var f struct {
		Tenant, Run, Actor, Session uuid.UUID
		Epoch                       int64
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	// Raw SQL tenant fixture bypasses normal tenant provisioning.
	// Keep native domain-first locking enabled; supply its required usage row.
	if _, err = admin.Exec(ctx, `INSERT INTO photo.tenant_usage(tenant_id) VALUES($1) ON CONFLICT(tenant_id) DO NOTHING`, f.Tenant); err != nil {
		t.Fatal("tenant usage fixture", err)
	}
	blocker, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `SELECT id FROM photo.tenants WHERE id=$1 FOR UPDATE`, f.Tenant); err != nil {
		t.Fatal(err)
	}
	tx, err := api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
```

