# Бэклог: API (BILL-10), часть 3

### API-15 — `POST /api/v1/me/deletion`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/me/deletion | fresh auth,ownership resolution,key | 202 request_id + limited status capability; avoid email in URL |

### API-16 — `GET /api/v1/me/deletion/{request}/status`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/me/deletion/{request}/status | scoped status token | Own request progress/retention exceptions; no global lookup |

### API-17 — `GET /api/v1/tenants/{t}/billing/offers`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/tenants/{t}/billing/offers | resource_code? |200 items BillingOffer, catalog_revision; owner only |

### API-18 — `POST /api/v1/tenants/{t}/billing/cost-quotes`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/tenants/{t}/billing/cost-quotes | workflow_kind,selection_snapshot_id,parameters_hash. key |201 cost quote id/revision/items/max cost. Existing workflow mutation принимает optional billing_cost_quote_id только для beta compatibility, mandatory enforce cohort |

### API-19 — `POST /api/v1/me/recent-auth`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/me/recent-auth | purpose,verified fresh OTP proof binding,key |201 opaque purpose-bound grant,expires_at; не создаёт вторую logged-in identity |

### API-20 — `POST /api/v1/me/deletion/preflight`

- Статус: целевая операция из 04/09. Наличие DTO или interface в registry не доказывает рабочий service; фактическая приёмка — STATUS.md.
- Вход: строка 04/09 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Тесты (в этой же задаче, R33, `TESTING.md`): Go-тест handler в `backend/internal/httpapi/*_test.go` — успех, 422, чужая роль, идемпотентный повтор; contract-тест, что endpoint и DTO есть в обоих реестрах `endpoints.json` и `schemas.json`. Без них задача не принята.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/me/deletion/preflight | key |200 privacy_case_id,revision,owned tenant choices,required recent-auth purpose |
