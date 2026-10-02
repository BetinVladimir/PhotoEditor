# Реализация: фактическое состояние

Это журнал разработки, а не заявление о готовности системы. Статус каждого пакета P00–P18 хранится в отдельном JSON; `coverage.json` перечисляет наличие реальных HTTP handlers. Наличие схемы или таблицы не считается работающей функцией. Итоговая приёмка P18 ещё не выполнена.

Разработка сохраняет исходный Python pipeline, presets и прежнее окружение. `make contracts-check` проверяет контрольные суммы из `resources/legacy-quality-baseline.json`. Команды `make test` и `make test-db` запускаются через `scripts/run-platform.sh`, который удерживает кеши внутри репозитория и поддерживает штатный macOS Make 3.81.

Интеграционные проверки используют отдельную временную базу на PostgreSQL из `work/postgres.env`; они не изменяют пользовательскую базу. Go unit test без этого окружения пропускает integration tests; `make test-db` запускает их на настоящей БД под ограниченными ролями.

Фактические миграции 00001–00009 создают таблицы каталога; 00010 — составные ограничения, RLS grants и immutable/owner triggers; 00011 — trusted auth bootstrap; 00012 — tenant-root RLS и membership lookup. Последующие служебные SQL функции оформляются отдельными последовательными миграциями. Migration 00167 атомарно создаёт Master/Web export presets через tenant trigger и backfill; canonical config hashes и live PostgreSQL bootstrap проверены.

Для native HTTP-клиента предусмотрен `X-Photo-Client: native` при отсутствии Origin и Fetch Metadata. Web использует exact Origin, bootstrap CSRF и session-bound CSRF. Refresh-токен web возвращается только в HttpOnly cookie; native получает его для SecureStore. Ключи загружаются из AUTH_KEYS_FILE с правами 0600; автоматической замены ключей при запуске нет.

Файл документации `contracts/endpoints.json` исправлен: OTP request не требует Idempotency-Key согласно нормативному тексту 04 (его защищают собственные rate limits и single-use semantics).

20.09.2026 исправлена ошибка разрешения пути platform-env.sh под zsh. Созданные этим запуском кеши из родительского каталога перенесены внутрь `.cache/` и `.runtime/`. Исходный код всё время создавался внутри PhotoEditor.

Проверка runtime: `work/evidence/runtime-suite.log` — legacy 22, platform 48, Go packages и integration 21 прошли; в integration входят PostgreSQL/RabbitMQ и Python worker transport. Миграции 00015–00016 добавляют fenced task leases/commits и recovery. Полный scheduler/object gateway и production wiring атомарных операций ещё не готовы.

Scheduler: миграции 00017–00018 добавляют physical node budgets, pinned operation catalog, atomic admission/outbox и weighted tenant fairness. Роль `PHOTO_ROLE=scheduler` запускает dispatch/recovery и health/metrics HTTP. `scheduler-integration-tests.log`: 29 тестов integration прошли; последующая проверка 14 runtime/Go integration в `scheduler-retry-tests.log` прошла. Graph compiler/event-consumer, object gateway и production operation wiring остаются в работе.

Контракты геометрии: `image-contract-platform-tests.log` — 52 platform tests прошли; JSON Schema WorkingImage/MaskRef/Depth/Face68/Pose17/Stats20/TransformChain генерируются из исполняемых моделей.

Gateway client: `gateway-platform-tests.log` — 64 platform tests прошли; `gateway-failure-integration-tests.log` — 15 task-runtime integration tests прошли. mTLS upload/download проверены локальным HTTPS-сервером; MinIO/server gateway пока не подключены.

MinIO: `objectstore-integration-tests.log` — прошли реальные versioned single/multipart upload, read/delete и конкурентная защита от перезаписи. Локальный сервер собран из закреплённого исправленного source release; данные macOS размещены на ограниченном sparse-томе внутри репозитория. Credentials: `gateway-http-tests.log` и `gateway-credentials-client-tests.log` подтверждают Go handler и Python refresh через mTLS. Полный object gateway с SQL attestation и подключением production worker остаётся в работе.

CPU image.color: глобальные gains/curves/saturation/residual LUT либо resolved absolute LUT применяются плитками256/512/1024 без промежуточной8-bit квантизации. Отдельный ColorReport содержит доли clipping по стадиям; immutable alpha, Canvas и TransformChain сохраняются. Strength намеренно завершается следующим image.local согласно спецификации07; этот следующий adapter ещё не реализован. Добавлена миграция33 политик трёх выходов, пока без проверки на живой PostgreSQL.

image.local подключён к CPU worker: semantic local, региональные коррекции и одно финальное linear strength blending. Проверяет aligned base и маски, сохраняет alpha, публикует skip/clipping evidence. image-local-platform-tests.log:185 passed; contracts-check и legacy SHA прошли. Миграция34 пока не проверена на живой БД; series, mask inference, graph и полный apply остаются в работе.

Guest portal: `apps/portal` содержит production-собираемый Next.js клиент без mock-data. Реализованы fragment→CSRF→HttpOnly session exchange, независимые view/download/upload capabilities, ограниченное 300 items окно, memory-only media URL cache, server selection snapshots, download intents и multipart uploads. Unit tests и production build проходят; live browser E2E с PostgreSQL/MinIO/API и ручная RU/BG/accessibility проверка остаются открытыми воротами P08.

P14 содержит CPU-операции `background.compose`, `background.blur`, `background.sky`, `hdr.window_pull`, `hdr.register`, `hdr.merge`, `sensor.dust` и GPU adapters `image.denoise`, `image.upscale`, `image.inpaint`, `image.generate`, `image.outpaint`: strict contracts/JSON Schema, linear-light background/HDR, SIFT/ECC+MTB bracket registration, calibrated Debevec tiled radiance+Reinhard, exact protected restoration, review-only generated candidates и wide-gamut safe model delta transfer. Tiled scratch lifecycle и migrations00102–00113 добавлены. Offline TorchScript/Diffusers factories и optional worker model-lock binding реализованы; без approved/qualified lock операции не рекламируются. Controlled-inference тесты проходят, реальные weights и hardware/visual gates не проверены.

P06 завершён по критериям UP01/UP02/UP03, фиксированным thumbnail/preview profiles и GC leases. UP01 и UP02 исполняются на живых PostgreSQL/MinIO; RAW camera corpus и target-hardware visual acceptance остаются внешними воротами P01/P18.
