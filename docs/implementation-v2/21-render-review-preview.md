# Контракт визуального render review

Этот документ уточняет контракт презентации существующего candidate-image review. Он не заменяет нормативный lifecycle09/15 и не объявляет review обязательным для всех preview workflows. Согласование расширенного render-review lifecycle и маршрутов reject/skip остаётся обязательной незавершённой работой.

Для decided `render.compose` добавлен отдельный GET `/api/v1/tenants/{t}/reviews/{id}/selected-preview/{variant}`. Он сначала читает подтверждённый render result selection, выбирает `before` для `base` и `after` для `candidate`, затем использует тот же физический manifest/image verifier и повторно читает selection после подписи. Для `skip` изображения нет (404). Ответ имеет форму ReviewPreview и `private, no-store`; обычный маршрут `/previews/{side}/{variant}` по-прежнему предназначен для pending review. Детальная запись review теперь доступна и для decided render, чтобы клиент мог прочитать decision hash и выбранный source.

## API и структуры

GET `/api/v1/tenants/{t}/reviews/{id}/previews/{side}/{variant}`: side before/after, variant preview1600/preview2560. Аутентифицированный пользователь должен иметь доступ к tenant/project. Только pending render.compose или optical.negative review; no-store. Для decided review обычный маршрут не выдаёт ни before, ни after; выбранный render-результат читается через отдельный selected-preview. Отсутствующий точный derivative возвращает404 и не разрешает показать произвольный run artifact вместо него.503 означает отсутствующую зависимость. Ошибки integrity не превращаются в успешный ответ.

ReviewPreview строго содержит review_id, side, source ArtifactRef, profile_hash, width, height, manifest, image. ReviewPreviewLink содержит artifact ArtifactRef, bytes, media_type, url, expires_at. Машинные определения находятся в contracts/schemas.json; маршрут — contracts/openapi.json. SHA относится к физическим байтам, а не к artifact ID. Manifest≤1MiB; image≤52MiB; geometry≤2560 для поддерживаемых вариантов. URL должен содержать ровно один непустой `versionId`, равный принятой сервером версии объекта, и живёт не более шести минут; сервис запрашивает пять минут. URL не является долговременным доказательством решения.

## Серверный алгоритм

1. SQLCandidatePreviewCatalog.Lookup читает текущий pending review под project authority, выбирает точный base/candidate. Для before допускаются принятые asset versions, master которых совпадает с base ID/SHA. Проверяет adopted/current task result, attempt/fence, output/upload tuples, trusted profile и готовые versioned objects.
2. CandidatePreviewService.Preview читает bounded source WorkingImage, проверяет physical SHA и strict codec. Читает manifest и проверяет canonical bytes/SHA, source/derivative/profile/geometry/alpha/MIME. Подписывает только точные versioned objects.
3. После I/O повторяет полный catalogue и source bytes, проверяет cancellation. HTTP handler проверяет review/side идентичность ответа; отсутствующий provider не выдаёт пустой успешный ответ.
4. before-media enqueue/claim/plan используют01409; финальная публикация01410 получает NOWAIT authority locks, повторяет readiness и child-run eligibility. Contention откладывает проход, stale authority отменяет job. Другие media origins сохраняют поведение.

## Клиентный алгоритм

ownRenderReviewBinding фиксирует pending revision и exact candidate/base. loadReviewPreview проверяет envelope, bounded manifest и image downloads с physical SHA. ReviewPreviewBytes сохраняет profile. prepareReviewPreviewPair требует одинаковые trusted profile и геометрию и разные source artifact IDs. prepareReviewPreviewDisplay декодирует проверенные bytes и сверяет фактическую геометрию. Перед монтированием пары экран повторяет свежий review и сравнивает revision/candidate/base. Abort/scope смена не могут установить старый результат. Ошибка или ещё неготовый before оставляют решение недоступным; Retry повторяет загрузку.

## Критерии последующей проверки

Написанные unit-тесты не считаются выполненными. Требуются positive SQL adoption→review→before-media→signed-preview lifecycle, реальные роли/двойной tenant, конкурентные decision/cancel/purge против публикации, malformed manifest/encoded bytes, stale attempt/fence/object version, browser decoder и повторная загрузка. Отдельно требуется завершить reject/skip и проверить соответствие всему normative lifecycle. Миграции01406–01410 пока не применялись; целый модуль не принят.
