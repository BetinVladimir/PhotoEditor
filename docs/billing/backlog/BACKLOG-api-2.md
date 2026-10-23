# Бэклог: API (BILL-10), часть 2

### API-12 — `GET /api/v1/legal/requirements`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/legal/requirements | locale,account kind | Active document URLs/versions/hash; no remote fetch per click |

### API-13 — `POST /api/v1/me/legal/acceptances`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/me/legal/acceptances | versions,explicit actions,key | Onboarding complete/pending; no auth activation without contract |

### API-14 — `PATCH /api/v1/me/privacy`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| PATCH /api/v1/me/privacy | expected_revision,optional preferences,key | Updated preference + event receipt |

### API-15 — `POST /api/v1/me/deletion`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| POST /api/v1/me/deletion | fresh auth,ownership resolution,key | 202 request_id + limited status capability; avoid email in URL |

### API-16 — `GET /api/v1/me/deletion/{request}/status`

- Статус: целевая identity из 04, в `endpoints.json` нет (проверено при генерации).
- Вход: строка 04 ниже; DTO и success status — 09 §7; образцы — `EXAMPLES-API.md`, `EXAMPLES-GO.md`.
- Выход: запись в оба `endpoints.json`; DTO в `schemas.json`; handler и сервисный интерфейс; маршрут в `httpapi`.
- Критерий: A. DTO `additionalProperties=false`, `required`, без server-only полей. B. role/tenant проверены (09 §2). C. Handler с `Idempotency-Key`/`If-Match`, где указано. D. Go-тест handler + drift DTO.
- Запреты: R7, R13; endpoint без рабочего handler не регистрировать.
- Из 04:

| GET /api/v1/me/deletion/{request}/status | scoped status token | Own request progress/retention exceptions; no global lookup |
