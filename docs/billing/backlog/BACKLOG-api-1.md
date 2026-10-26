# Бэклог: API (BILL-10), часть 1

### API-01 — `GET /api/v1/billing/catalog`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/billing/catalog | locale,currency | Public published plans/offers; Custom исключён; cache version |

### API-02 — `GET /api/v1/tenants/{t}/billing`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/tenants/{t}/billing | none | Current plan, prices/status/period, changes, capability flags, PII только owner |

### API-03 — `GET /api/v1/tenants/{t}/billing/usage`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/tenants/{t}/billing/usage | resource,cursor? | used/leased/available/overrun, storage categories, egress, updated_at и revision; owner/admin read-only policy |

### API-04 — `GET /api/v1/tenants/{t}/billing/buckets`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/tenants/{t}/billing/buckets | resource,cursor | Paginated buckets+expiry; owner/admin |

### API-05 — `POST /api/v1/tenants/{t}/billing/quotes`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/tenants/{t}/billing/quotes | selection,counts,expected_revision,key | Quote id/hash/expires, строки, gross/net/tax, rights and effective dates |

### API-06 — `POST /api/v1/tenants/{t}/billing/orders`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/tenants/{t}/billing/orders | quote_id,Idempotency-Key,If-Match |202 stable order_id/status; hosted action URL через GET order; owner only |

### API-07 — `GET /api/v1/tenants/{t}/billing/orders/{order}`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/tenants/{t}/billing/orders/{order} | none | Local authoritative status, safe next action; foreign 404 |

### API-08 — `POST /api/v1/tenants/{t}/billing/changes`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/tenants/{t}/billing/changes | quote_id,key,expected_revision | Scheduled/applied/pending change; owner |
