# Построчный статический аудит контрактных трасс

Срез `1787516`. Метод: source inspection only; no tests, migrations, builds or runtime calls.

Охват: 30/30 операций имеют прямую или динамическую worker-регистрацию; 79/79 HTTP-контрактов имеют объявление маршрута, достижимое из `Server.Routes()` через вызовы методов. Ни одного отсутствующего structural declaration не найдено.

В 79 route handlers найдены кандидаты входа в авторизацию: 72 `tenantRequest`, 4 проверяющих отзывные media grants, 2 device challenge/token и 1 device principal для capture receipt. Эта классификация не доказывает ACL внутри сервисов/SQL.

`declared` означает только найденную регистрацию/маршрут в исходниках. Все поведенческие ворота остаются не подтверждёнными без отдельного анализа/исполнения; отсутствие нового defect ID не означает готовность.

## Операции

| Пакет | Операция | Worker declaration | Service/SQL literal candidates | Дефект/блокер |
|---|---|---|---|---|
| V25 | `layer.compose` | direct_literal | 6/9 | — |
| V22 | `session.describe` | direct_literal | 9/7 | — |
| V22 | `session.plan` | direct_literal | 3/5 | — |
| V23 | `learning.fit` | direct_literal | 4/20 | — |
| V23 | `learning.evaluate` | direct_literal | 3/9 | — |
| V24 | `capture.assess` | direct_literal | 5/13 | — |
| V23 | `identity.compare` | direct_literal | 5/20 | D-02: learning/identity report client route and review are absent |
| V27 | `semantic.transfer` | direct_literal | 26/42 | D-01: proposed target lacks pre-Apply candidate/evidence presentation; D-04: mandatory MVP CUDA qualification on dedicated RTX |
| V28 | `portrait.open_eyes` | direct_literal | 17/21 | — |
| V28 | `portrait.glare` | direct_literal | 22/25 | D-04: qualify CPU/CUDA deployment; MPS is not required for MVP |
| V28 | `image.reflection` | direct_literal | 21/24 | D-04: qualify CPU/CUDA deployment; MPS is not required for MVP |
| V28 | `portrait.hair` | direct_literal | 13/11 | — |
| V28 | `portrait.clothing` | direct_literal | 13/11 | — |
| V28 | `image.relight` | direct_literal | 5/9 | — |
| V25 | `optical.lens` | direct_literal | 5/13 | — |
| V29 | `optical.perspective` | direct_literal | 30/39 | — |
| V29 | `optical.negative` | direct_literal | 69/50 | — |
| V29 | `optical.panorama` | dynamic_named_dispatch | 42/34 | — |
| V29 | `optical.focus_stack` | dynamic_named_dispatch | 38/34 | — |
| V31 | `provenance.sign` | direct_literal | 4/21 | — |
| V30 | `v2.delivery.assemble` | dynamic_named_dispatch | 12/15 | — |
| V30 | `v2.interchange.import` | direct_literal | 2/11 | — |
| V30 | `v2.interchange.export` | direct_literal | 2/13 | — |
| V27 | `v2.semantic.assemble` | direct_literal | 3/15 | — |
| V30 | `v2.interchange.parse` | direct_literal | 2/21 | — |
| V30 | `v2.delivery.prepare` | dynamic_named_dispatch | 6/23 | — |
| V26 | `region.preview` | direct_literal | 3/16 | D-04: mandatory MVP CUDA qualification on dedicated RTX |
| V26 | `render.compose` | direct_literal | 49/75 | — |
| V27 | `semantic.local_exposure` | direct_literal | 3/20 | — |
| V29 | `optical.perspective_lines` | direct_literal | 2/20 | — |

## HTTP-контракты

| Пакет | Метод и путь | Route declaration | Auth entry candidate | Дефект |
|---|---|---|---|---|
| V22 | `POST /api/v1/tenants/{t}/shoot-sessions` | direct | tenant_request | — |
| V22 | `GET /api/v1/tenants/{t}/shoot-sessions/{id}` | direct | tenant_request | — |
| V22 | `GET /api/v1/tenants/{t}/shoot-sessions` | direct | tenant_request | — |
| V22 | `PATCH /api/v1/tenants/{t}/shoot-sessions/{id}` | direct | tenant_request | — |
| V22 | `POST /api/v1/tenants/{t}/shoot-sessions/{id}/analyses` | direct | tenant_request | — |
| V22 | `GET /api/v1/tenants/{t}/session-analyses/{id}` | direct | tenant_request | — |
| V22 | `POST /api/v1/tenants/{t}/session-analyses/{id}/apply` | direct | tenant_request | — |
| V23 | `POST /api/v1/tenants/{t}/corrections` | direct | tenant_request | — |
| V23 | `POST /api/v1/tenants/{t}/learning-profiles` | direct | tenant_request | — |
| V23 | `GET /api/v1/tenants/{t}/learning-profiles/{id}` | direct | tenant_request | — |
| V23 | `POST /api/v1/tenants/{t}/learning-profiles/{id}/train` | direct | tenant_request | — |
| V23 | `GET /api/v1/tenants/{t}/learning-profiles/{id}/versions` | direct | tenant_request | — |
| V23 | `POST /api/v1/tenants/{t}/learning-profiles/{id}/activate` | direct | tenant_request | — |
| V22 | `POST /api/v1/tenants/{t}/projects/{id}/consents` | direct | tenant_request | — |
| V22 | `GET /api/v1/tenants/{t}/projects/{id}/consents` | dynamic_table | tenant_request | — |
| V22 | `POST /api/v1/tenants/{t}/project-persons` | direct | tenant_request | — |
| V22 | `GET /api/v1/tenants/{t}/projects/{id}/persons` | dynamic_table | tenant_request | — |
| V22 | `POST /api/v1/tenants/{t}/project-persons/{id}/observations` | direct | tenant_request | — |
| V22 | `POST /api/v1/tenants/{t}/project-persons/{id}/revoke` | direct | tenant_request | — |
| V24 | `PUT /api/v1/tenants/{t}/shoot-sessions/{id}/shot-list` | direct | tenant_request | — |
| V24 | `GET /api/v1/tenants/{t}/shoot-sessions/{id}/shot-list` | direct | tenant_request | — |
| V24 | `POST /api/v1/tenants/{t}/capture-devices` | direct | tenant_request | — |
| V24 | `POST /api/v1/tenants/{t}/capture-devices/{id}/revoke` | direct | tenant_request | — |
| V24 | `POST /api/v1/tenants/{t}/capture-devices/{id}/challenges` | direct | device_challenge_exchange | — |
| V24 | `POST /api/v1/tenants/{t}/capture-devices/{id}/token` | direct | device_challenge_exchange | — |
| V24 | `POST /api/v1/tenants/{t}/shoot-sessions/{id}/capture-receipts` | direct | device_principal | — |
| V24 | `GET /api/v1/tenants/{t}/shoot-sessions/{id}/alerts` | direct | tenant_request | — |
| V24 | `POST /api/v1/tenants/{t}/capture-receipts/{id}/confirm` | direct | tenant_request | — |
| V24 | `POST /api/v1/tenants/{t}/capture-alerts/{id}/ack` | direct | tenant_request | — |
| V21 | `PUT /api/v1/tenants/{t}/assets/{id}/v2-draft` | direct | tenant_request | — |
| V21 | `GET /api/v1/tenants/{t}/assets/{id}/v2-draft` | direct | tenant_request | — |
| V21 | `GET /api/v1/tenants/{t}/assets/{id}/v2-binding` | direct | tenant_request | — |
| V21 | `GET /api/v1/tenants/{t}/assets/{id}/v2-draft/history` | direct | tenant_request | — |
| V21 | `POST /api/v1/tenants/{t}/assets/{id}/v2-draft/approve` | direct | tenant_request | — |
| V26 | `POST /api/v1/tenants/{t}/assets/{id}/v2-draft/freeze` | direct | tenant_request | — |
| V26 | `POST /api/v1/tenants/{t}/projects/{id}/render-input-snapshots` | direct | tenant_request | — |
| V26 | `GET /api/v1/tenants/{t}/projects/{id}/render-policy` | direct | tenant_request | — |
| V26 | `POST /api/v1/tenants/{t}/assets/{id}/region-previews` | direct | tenant_request | — |
| V26 | `POST /api/v1/tenants/{t}/projects/{id}/render-batches` | direct | tenant_request | — |
| V28 | `POST /api/v1/tenants/{t}/projects/{id}/relight-analyses` | direct | tenant_request | — |
| V28 | `POST /api/v1/tenants/{t}/projects/{id}/relight-preparation` | direct | tenant_request | — |
| V28 | `POST /api/v1/tenants/{t}/projects/{id}/relight-candidates` | direct | tenant_request | — |
| V28 | `POST /api/v1/tenants/{t}/projects/{id}/relight-intents` | direct | tenant_request | — |
| V27 | `POST /api/v1/tenants/{t}/semantic-intents` | direct | tenant_request | — |
| V27 | `GET /api/v1/tenants/{t}/semantic-intents/{id}/targets` | direct | tenant_request | D-01: pre-Apply candidate/evidence presentation is absent |
| V27 | `POST /api/v1/tenants/{t}/semantic-intents/{id}/apply` | direct | tenant_request | D-01: pre-Apply candidate/evidence presentation is absent |
| V30 | `POST /api/v1/tenants/{t}/delivery-packages` | direct | tenant_request | — |
| V30 | `GET /api/v1/tenants/{t}/delivery-packages/{id}` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/delivery-packages/{id}/publish` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/interchange-jobs` | direct | tenant_request | — |
| V30 | `GET /api/v1/tenants/{t}/interchange-jobs/{id}` | direct | tenant_request | — |
| V31 | `POST /api/v1/tenants/{t}/exports/{id}/provenance` | direct | tenant_request | — |
| V21 | `GET /api/v1/tenants/{t}/v2-documents/{id}` | direct | tenant_request | — |
| V21 | `GET /api/v1/tenants/{t}/v2-workflows/{id}` | direct | tenant_request | — |
| V21 | `POST /api/v1/tenants/{t}/v2-workflows/{id}/cancel` | direct | tenant_request | — |
| V27 | `GET /api/v1/tenants/{t}/runs/{id}/semantic-intent` | direct | tenant_request | — |
| V26 | `GET /api/v1/tenants/{t}/assets/{id}/region-previews/{run}/descriptor` | direct | tenant_request | — |
| V26 | `GET /api/v1/tenants/{t}/assets/{id}/v2-layers/{artifact}` | direct | tenant_request | — |
| V26 | `GET /api/v1/tenants/{t}/shoot-sessions/{id}/analyses` | direct | tenant_request | — |
| V26 | `GET /api/v1/tenants/{t}/session-analyses/{id}/scene-board` | direct | tenant_request | — |
| V27 | `POST /api/v1/tenants/{t}/semantic-intents/{id}/close-review` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/interchange-jobs/{id}/decisions` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/interchange-jobs/{id}/close-review` | direct | tenant_request | — |
| V23 | `POST /api/v1/tenants/{t}/learning-versions/{id}/safety-review` | direct | tenant_request | — |
| V29 | `GET /api/v1/tenants/{t}/projects/{id}/reviews/{review}/perspective-replacements/{original}` | direct | tenant_request | — |
| V29 | `GET /api/v1/tenants/{t}/reviews/{review}/perspective-replacements` | direct | tenant_request | — |
| V29 | `GET /api/v1/tenants/{t}/projects/{id}/reviews/{review}/multi-replacements/{original}` | direct | tenant_request | — |
| V29 | `GET /api/v1/tenants/{t}/reviews/{review}/multi-replacements` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/parts` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/complete` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}/abort` | direct | tenant_request | — |
| V30 | `POST /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/attempt-status` | direct | tenant_request | — |
| V30 | `GET /api/v1/tenants/{t}/projects/{id}/interchange-archive-uploads/{upload}` | direct | tenant_request | — |
| V25 | `GET /api/v1/tenants/{t}/projects/{id}/raw-capabilities` | direct | tenant_request | — |
| V26 | `GET /api/v1/media/review-pending` | direct | signed_revocable_media_grant | — |
| V26 | `GET /api/v1/media/render-selected` | direct | signed_revocable_media_grant | — |
| V28 | `GET /api/v1/media/restoration-selected` | direct | signed_revocable_media_grant | — |
| V26 | `GET /api/v1/media/run-output` | direct | signed_revocable_media_grant | — |

Полные пути файлов и кандидаты исходников находятся в [static-link-review.json](static-link-review.json) и [trace-inventory.json](trace-inventory.json). Подтверждённые дефекты и условия исправления — в [static-link-defects-2026-10-15.md](static-link-defects-2026-10-15.md).
