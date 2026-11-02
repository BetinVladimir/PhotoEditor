# Бэклог: API (BILL-10), часть 2

### API-08 — `POST /api/v1/tenants/{t}/billing/changes`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/tenants/{t}/billing/changes | quote_id,key,expected_revision | Scheduled/applied/pending change; owner |

### API-09 — `POST /api/v1/tenants/{t}/billing/changes/{change}/cancel`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/tenants/{t}/billing/changes/{change}/cancel | expected_revision,key | Cancel future change; paid saga требует compensation, не простой delete |

### API-10 — `POST /api/v1/tenants/{t}/billing/portal`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/tenants/{t}/billing/portal | return_route enum,Idempotency-Key |202 portal_action_id; fresh short-lived URL через GET action; owner; no-store |

### API-11 — `POST /api/v1/webhooks/stripe/{account}`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/webhooks/stripe/{account} | raw body + signature | 2xx только после durable inbox; invalid signature 400; storage failure 5xx |

### API-12 — `GET /api/v1/legal/requirements`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/legal/requirements | locale,account kind | Active document URLs/versions/hash; no remote fetch per click |

### API-13 — `POST /api/v1/me/legal/acceptances`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/me/legal/acceptances | versions,explicit actions,key | Onboarding complete/pending; no auth activation without contract |

### API-14 — `PATCH /api/v1/me/privacy`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| PATCH /api/v1/me/privacy | expected_revision,optional preferences,key | Updated preference + event receipt |
