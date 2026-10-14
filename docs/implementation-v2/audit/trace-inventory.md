# Текущий реестр кандидатов трасс V20–V32

Машинный реестр: [trace-inventory.json](trace-inventory.json). Он перечисляет все операции и API-контракты, но ни одна строка не считается принятой по статическому совпадению.

Операции: 30; endpoint-контракты: 79. Кандидатов worker без точного литерала: 0; endpoint без статического route-кандидата: 0; без точного имени handler в API: 49 (включая inline handlers).

## Следующие ручные проверки

Для каждой строки требуется проследить mounted route и клиент, полномочия и frozen входы, admission/dispatch, worker result, transactional adoption, review/download, отказ/повтор и физический GC/purge. Отсутствие точного литерала может означать косвенную регистрацию; наличие литерала не доказывает работу.

### Операции без точного worker-кандидата

- Нет по этому статическому признаку.

### Endpoint-контракты без статического route-кандидата

- Нет по этому статическому признаку.

### Endpoint-контракты без точного API handler-кандидата

- `POST /api/v1/tenants/{t}/shoot-sessions`
- `GET /api/v1/tenants/{t}/shoot-sessions/{id}`
- `PATCH /api/v1/tenants/{t}/shoot-sessions/{id}`
- `POST /api/v1/tenants/{t}/shoot-sessions/{id}/analyses`
- `GET /api/v1/tenants/{t}/projects/{id}/consents`
- `POST /api/v1/tenants/{t}/project-persons`
- `GET /api/v1/tenants/{t}/projects/{id}/persons`
- `POST /api/v1/tenants/{t}/project-persons/{id}/observations`
- `POST /api/v1/tenants/{t}/project-persons/{id}/revoke`
- `POST /api/v1/tenants/{t}/capture-devices`
- `POST /api/v1/tenants/{t}/capture-devices/{id}/revoke`
- `PUT /api/v1/tenants/{t}/assets/{id}/v2-draft`
- `GET /api/v1/tenants/{t}/assets/{id}/v2-binding`
- `GET /api/v1/tenants/{t}/assets/{id}/v2-draft/history`
- `POST /api/v1/tenants/{t}/assets/{id}/v2-draft/approve`
- `POST /api/v1/tenants/{t}/assets/{id}/v2-draft/freeze`
- `POST /api/v1/tenants/{t}/projects/{id}/render-input-snapshots`
- `GET /api/v1/tenants/{t}/projects/{id}/render-policy`
- `POST /api/v1/tenants/{t}/assets/{id}/region-previews`
- `POST /api/v1/tenants/{t}/projects/{id}/render-batches`
- `POST /api/v1/tenants/{t}/projects/{id}/relight-analyses`
- `POST /api/v1/tenants/{t}/projects/{id}/relight-preparation`
- `POST /api/v1/tenants/{t}/projects/{id}/relight-candidates`
- `POST /api/v1/tenants/{t}/projects/{id}/relight-intents`
- `GET /api/v1/tenants/{t}/semantic-intents/{id}/targets`
- `POST /api/v1/tenants/{t}/semantic-intents/{id}/apply`
- `GET /api/v1/tenants/{t}/delivery-packages/{id}`
- `POST /api/v1/tenants/{t}/delivery-packages/{id}/publish`
- `GET /api/v1/tenants/{t}/interchange-jobs/{id}`
- `GET /api/v1/tenants/{t}/v2-workflows/{id}`
- `POST /api/v1/tenants/{t}/v2-workflows/{id}/cancel`
- `GET /api/v1/tenants/{t}/assets/{id}/region-previews/{run}/descriptor`
- `GET /api/v1/tenants/{t}/shoot-sessions/{id}/analyses`
- `POST /api/v1/tenants/{t}/semantic-intents/{id}/close-review`
- `GET /api/v1/tenants/{t}/projects/{id}/reviews/{review}/perspective-replacements/{original}`
- `GET /api/v1/tenants/{t}/reviews/{review}/perspective-replacements`
- `GET /api/v1/tenants/{t}/projects/{id}/reviews/{review}/multi-replacements/{original}`
- `GET /api/v1/tenants/{t}/reviews/{review}/multi-replacements`
- `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads`
- `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/parts`
- `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/complete`
- `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/abort`
- `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/attempt-status`
- `GET /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}`
- `GET /api/v1/tenants/{t}/projects/{id}/raw-capabilities`
- `GET /api/v1/media/review-pending`
- `GET /api/v1/media/render-selected`
- `GET /api/v1/media/restoration-selected`
- `GET /api/v1/media/run-output`

Этот список не означает отсутствующий endpoint: inline-функции и динамические таблицы маршрутов не содержат нормативное имя handler. Каждое совпадение и статус доступны в JSON; дальнейшая проверка должна проставить доказательство по слоям без переноса исторических PASS.
