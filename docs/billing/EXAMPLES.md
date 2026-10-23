# EXAMPLES — образцы артефактов

Образцы взяты из реальных файлов `contracts/`. Копировать форму, не содержимое.

## 1. SQL-функция с ACL (из `contracts/routed-workflow.sql`)

```sql
CREATE OR REPLACE FUNCTION photo.billing_prepare_apply(p_tenant uuid,p_actor uuid,p_session uuid,p_route text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 -- 1. R18: сверка tenant и authority
 IF nullif(current_setting('app.tenant_id',true),'')::uuid IS DISTINCT FROM p_tenant THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID'; END IF;
 -- 2. роль из authorize_tenant, иначе тот же отказ
 -- 3. неизвестный маршрут -> RETURN false (не исключение)
 -- 4. cohort включён? иначе RETURN false
 -- 5. R23: billing_quota_lock(p_tenant,'standard_photo') до остальных блокировок
 -- 6. повторная проверка authority ПОСЛЕ ожидания mutex
 -- 7. bounded recovery (LIMIT 16), затем RETURN true
END $$;

ALTER FUNCTION photo.billing_prepare_apply(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.billing_prepare_apply(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.billing_prepare_apply(uuid,uuid,uuid,text) TO photo_api;
```

Что копировать: заголовок (`SECURITY DEFINER`, `SET search_path`), три строки ACL после тела, порядок «authority → cohort → lock → authority ещё раз».

## 2. Запись в `*-functions.json` (из `contracts/workflow-functions.json`)

```json
{
  "schema_version": 1,
  "scope": "standard_apply",
  "functions": [
    { "signature": "billing_prepare_apply(uuid,uuid,uuid,text)", "owner": "photo_fn_owner", "execute_roles": ["photo_api"] },
    { "signature": "billing_lock_apply_run(uuid,uuid)",          "owner": "photo_fn_owner", "execute_roles": [] }
  ]
}
```

Правило: `signature` — типы аргументов без имён; `execute_roles` строго равен `GRANT EXECUTE` в SQL; helper → пустой массив. Совпадение проверяют contract-тесты и `--check`.

## 3. Миграция (шаблон заголовка)

```sql
-- +goose Up
-- Только additive. Старые миграции не трогать (R9).
-- Перед CREATE OR REPLACE существующей функции: проверка hash/signature/owner/ACL (R29).
```

Имя файла: `0NNNN_billing_<что>.sql`. Номер = следующий после последнего файла в `backend/db/migrations/` (проверить: `ls backend/db/migrations | tail -1`). На 23.10.2026 head = `02656`, значит следующая миграция `02657`. Билинг-миграции 02598–02609 уже заняты; между ними и head лежат небиллинговые миграции 02610–02656, их не трогать.

## 4. Остальные образцы (генерируются из живого кода)

- Go-обёртка над SQL, MapError, HTTP-handler: `EXAMPLES-GO.md`.
- Endpoint (v1 и v2) и DTO в `schemas.json`: `EXAMPLES-API.md`.
- Python integration-тест и Go-harness: `EXAMPLES-TESTS.md`.

Обновление: `python3 docs/billing/tools/gen-examples.py`; проверка актуальности: `--check`.
