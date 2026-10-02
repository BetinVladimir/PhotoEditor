# PhotoEditor

Локальный пакетный редактор портретов для macOS / Apple Silicon. Он приводит серию к общей яркости, корректирует цвет и локальный свет, ретуширует кожу и выполняет ограниченную коррекцию контуров лица и тела. Оригинальное разрешение обрабатывается тайлами, а MediaPipe используется для поиска лиц и построения масок кожи.

Текущая версия — **0.3.3**, категория пайплайна — **«улучшение качества»**. После детерминированной обработки blur-gate направляет только нерезкие лица в CodeFormer, а ArcFace проверяет сохранение личности. Финальный полноразмерный проход выравнивает баланс белого, сжимает опасные света, приглушает остаточные блики и восстанавливает естественный цвет кожи на всех найденных лицах, включая восстановленные моделью.

## Быстрый запуск

```bash
cd /Users/freelancer/Documents/PhotoEditor
./run-e2e.sh
```

Первый запуск создаёт `.venv`, устанавливает закреплённые зависимости и скачивает две официальные модели. Нужны Python 3.10–3.12 и интернет при установке; проверено с Python 3.10.8 и 3.12.13 на M2 Max. Предпочтителен `uv`; при отсутствии используется pip. Все фото обрабатываются локально, повторный рендер работает offline.

Готовые отредактированные JPEG появляются непосредственно в `e2e/output/` под исходными именами. Версионированные результаты сохраняются в `e2e/output/runs/<run_id>/`: `images/` содержит 16-bit TIFF sRGB, `delivery/` — базовый JPEG, `finished/` — финальный JPEG после модельного восстановления и очистки бликов, `previews/` — прокси, `review/` — материалы проверки. Путь последнего run и список опубликованных файлов записываются в `e2e/output/latest.json`.

Код выхода **3** означает ошибки отдельных кадров или необходимость визуальной проверки; результат мог быть успешно сохранён. Это намеренное поведение: warp и невосстановимые пересветы не объявляются автоматически прошедшими приёмку. `report.json` различает `failed`, `needs_review` и `rendered`, а `acceptance.json` — техническую и визуальную приёмку.

## Команды

```bash
./run.sh validate
./run.sh analyze
./run.sh render
./run.sh render --resume
./run.sh preview --image 06_seaside_sun_dress.jpg
./run.sh schema
./run.sh setup-models
./scripts/setup-codeformer.sh
```

`run.sh` по умолчанию использует тестовый набор; для собственной коллекции:

```bash
export PHOTO_INPUT_DIR='/absolute/path/incoming'
export PHOTO_OUTPUT_DIR='/absolute/path/processed'
export PHOTO_CONFIG_FILE='/absolute/path/config.yaml'
./run.sh render
```

Для своей коллекции начните с `configs/natural.yaml`. Этот профиль сохраняет геометрию, выравнивает экспозицию серии и включает умеренную ретушь. E2E preset содержит имена шести конкретных файлов, ожидаемое число лиц, индивидуальные WB и размеченные области тела; на произвольной коллекции он намеренно не проходит validation.

В конфиге каждый эффект выключается независимо. `beauty.preserve_geometry: true` запрещает включать `geometry.enabled`. Без деформации можно использовать `contour_light`. Полная действующая схема — `schemas/config.schema.json`; неизвестные ключи отклоняются. Концептуальные YAML из архитектурной документации описывают более широкий будущий API и не являются готовыми конфигами версии 0.2.

Основные настройки:

- `collection`: общий уровень яркости лиц, сила нормализации серии и максимальная EV-коррекция;
- `color`: экспозиция, баланс белого, контраст, насыщенность и гамма;
- `beauty.skin`: сглаживание кожи лица и тела с сохранением текстуры;
- `beauty.even_light`: выравнивание крупных и локальных перепадов света на лице;
- `beauty.specular`, `redness`, `blemishes`: блики, общая краснота и локальные красные дефекты;
- `beauty.highlight_cleanup`: финальное удаление остаточных бликов после CodeFormer; сила, порог, локальный контраст и максимальное затемнение;
- `beauty.post_white_balance`: ограниченный gray-world баланс белого по нейтральным средним тонам и плавная защита светов;
- `beauty.face_color_finish`: адаптивное восстановление насыщенности и теплоты кожи относительно средних тонов каждого лица;
- `geometry`: ограниченное сужение контуров лица и размеченных областей тела;
- `detail.sharpening`: финальная резкость с защитой кожи;
- `detail.blur_detection`: порог нерезкости, автоматический CodeFormer, набор fidelity, сила смешивания и минимальное ArcFace-сходство;
- `image_overrides`: индивидуальные настройки для отдельных фотографий.

## Редактор

```bash
./scripts/setup.sh --studio
./run.sh studio
```

Начальный PySide6-редактор: список фотографий, before/after, редактирование всего YAML, валидация, сохранение, preview выбранного кадра, пакетный запуск и отмена. Worker работает отдельным процессом. Несохранённые параметры передаются временным снимком конфига. Preview — proxy; 100% crops доступны после экспорта в `review/faces`. Ползунки, кисти масок, интерактивный 100% ROI и развитое управление группами из архитектуры ещё не реализованы.

## Что работает

- JPEG/PNG и RGB TIFF; 8-bit вход с ICC-конвертацией; 16-bit interleaved RGB TIFF в sRGB без снижения битности.
- Двухпроходный анализ всей коллекции, exposure matching по медиане лиц или заданному target; ручные WB-gains и overrides кадров.
- MediaPipe landmarks с поиском на нескольких масштабах; face/body skin confidence masks, защита глаз, губ и бровей.
- Многомасштабное выравнивание света и мягкая тоновая компрессия лица с растушёванной маской, без резкой границы у волос.
- Подавление жирного блеска, общей красноты и локальных красных дефектов, сглаживание с сохранением высоких частот, contour lighting.
- Healing явно размеченных небольших областей; отдельный ограниченный face/body warp; резкость с защитой кожи.
- Full-resolution float32 tile pipeline, 16-bit TIFF ZIP, JPEG 4:4:4, встроенный sRGB ICC, атомарная запись, исходники не меняются.
- Кэш анализа с checksum, frozen plan, resume с проверкой выходных hashes, изоляция повреждённых фото, progress JSON Lines, contact sheet и crops всех лиц.
- Нормализованная оценка резкости каждого лица. Нерезкие лица получают отдельный QA-флаг с рекомендуемыми параметрами восстановления.
- Условная `detail.face_recovery`: локальный двухмасштабный high-pass до beauty-сглаживания, с маской лица, noise threshold и защитой пересветов.
- Опциональный CodeFormer в изолированном Python 3.10 runtime: полноразмерные face-crops, несколько fidelity-кандидатов, ArcFace identity gate, контроль резкости/клиппинга и безопасный откат.

## Границы реализации

Нет генеративного переосвещения. CodeFormer может синтезировать правдоподобную мелкую фактуру, но не возвращает доказуемо исходные детали и поэтому защищён ArcFace-gate и откатом. Нельзя достоверно восстановить выбитые участки JPEG или полностью заменить жёсткий рисунок солнца студийным. Автоматический blemish-фильтр действует только на небольшие красные отклонения; поры, родинки и прочие устойчивые черты он сохраняет. Отдельный дефект можно разметить в `beauty.blemishes.circles` как нормализованные `x, y, radius`, где радиус задан относительно ширины.

Geometry — экспериментальная деформация контура, требует просмотра фона/анатомии; для тела нужны явные `body_regions`, автоматического выделения талии по позе пока нет. Не обещается физически точная оценка тела. Общий target экспозиции не умеет автоматически отличать собственный тон кожи от недоэкспозиции: используйте `median`, индивидуальные overrides и визуальную проверку. Различные сцены не превращаются автоматически в одну физическую схему освещения.

Рабочее пространство версии 0.2 — linear sRGB float32, экспорт sRGB. Wide-gamut linear Rec.2020/ProPhoto из архитектуры, RAW, автоматический WB, teeth/sclera effects, сохранение EXIF и полная студийная приёмка по golden references ещё не реализованы. Неподдержанные high-bit ICC/HDR/multipage/alpha не преобразуются молча.

На macOS MediaPipe создаёт графический контекст даже для CPU-инференса; запускать в обычной пользовательской сессии. При работе из sandbox может потребоваться разрешение запуска вне него. Runtime закреплён на MediaPipe 0.10.21.

## Проверки

```bash
.venv/bin/python -m pytest -q
.venv/bin/python scripts/verify-e2e.py
```

Фактический E2E версии 0.3.3: 22 теста прошли, обработаны 6 фотографий 6000×4000 и 10 лиц, техническая проверка — PASS. В семейном кадре CodeFormer автоматически улучшил два нерезких лица; затем финальная очистка бликов обработала все лица коллекции. В `06_seaside_sun_dress.jpg` очистка скорректировала 31 087 пикселей бликов, а цветовой этап — 113 114 пикселей кожи без выходного клиппинга. Шесть файлов `finished/*.jpg` опубликованы непосредственно в `e2e/output`. Итоговый run: `e2e/output/runs/20260920-023305-4db99358`.

Техническая проверка имеет статус `PASS`. Визуальный статус остаётся `NEEDS_REVIEW`, когда исходник содержит невосстановимые пересветы или применена геометрия: это защитная проверка, а не ошибка обработки.

Полное описание текущего тракта находится в `docs/current-pipeline.md`, история реализации — в `docs/implementation.md`, требования к полной визуальной приёмке — в `docs/e2e-studio-series.md`.

## Восстановление нерезких лиц

Blur-gate измеряет variance of Laplacian на face-crop, приведённом к 512×512. Если значение ниже `detail.blur_detection.threshold`, кадр получает `face_blur_detected` и остаётся на визуальной проверке.

При `auto_restore: true` отдельный runtime пробует `fidelity_candidates` (по умолчанию 0.85, 0.92, 0.97) на каждом выровненном crop 512×512, полученном из полноразмерного результата. ArcFace IR-SE50 сравнивает исходный crop, CodeFormer-кандидат и смешанный результат. Принимается лучший кандидат с `identity_similarity >= 0.75`, приростом резкости не менее 5% и без существенного роста клиппинга; затем он смешивается с оригиналом (`blend: 0.65`) и вклеивается через parsing-mask. Если любая проверка не проходит, остаётся детерминированный JPEG. Установка весов выполняется явно командой `./scripts/setup-codeformer.sh`.

## Разработка платформы по ТЗ 4.1

Новая платформа находится в `backend/`, `workers/`, `packages/`, `resources/`. Её реализация продолжается; она пока не заменяет локальный пайплайн выше. Фактический объём и оставшиеся части записаны в `docs/implementation-progress/`, нормативные контракты — в `docs/implementation/`. Наличие JSON Schema или записи в реестре операций не означает готовую пользовательскую функцию.

```bash
make setup-platform
make build-minio
make dev-infra
make test
make test-db
make deploy-check
```

Эти команды используют отдельное Python 3.11 окружение `.venv-platform`; кеши и данные dev-сервисов размещаются внутри репозитория. PostgreSQL, RabbitMQ и MinIO привязаны к loopback. Интеграционные тесты создают и удаляют только собственные временные БД/vhost. Исходники и пресеты прежнего пайплайна проверяются по SHA256 (`make contracts-check`).

Исполняемая single-node production-топология находится в `deploy/compose.yaml`; подготовка секретов, обязательные immutable image digests, build arguments и профили `mail`/`cuda` описаны в `deploy/README.md`. Compose оставляет наружу только Caddy 80/443, запускает migrate/storage/queue bootstrap как one-shot barriers, использует внутреннюю сеть для DB/Rabbit/MinIO/gateway и включает healthchecks для API и фоновых ролей. `make deploy-check` проверяет состав ролей, nonroot/read-only ограничения, mTLS worker wiring, distinct worker IDs, profile isolation и отсутствие mutable/default-secret образов. Реальный production build, TLS authority, restore drill и аппаратные benchmarks остаются внешними release gates.

Go entrypoint `backend/cmd/photo` поддерживает роли `migrate`, `bootstrap-queue`, `api`, `publisher`, `dead-letter-worker`, `mail-worker`, `collection-worker`, `scheduler`, `object-gateway`, `maintenance`, `bootstrap-storage` через `PHOTO_ROLE`. Конфигурация и credentials задаются явно: `DATABASE_URL` с отдельной ролью сервиса, для очередей `AMQP_URL`; API требует отдельные auth credentials/keys. Scheduler выполняет resource admission, отправку ready-задач через outbox и recovery истёкших аренд. Collection worker последовательно потребляет durable `collection.mutate` commands из `q.worker.collection`; command связан с сохранённым event fence, поэтому повторная доставка не применяет chunk повторно. Для dispatch нужны заранее разрешённые node/worker, свежие capacity reports и квалификация конкретной пары operation/backend. Компиляция/advance графов, object gateway, полные API/клиенты и production wiring всех операций ещё не завершены.

Новый численный тракт включает рабочие ICC, tiled decode, цветовые преобразования NumPy/PyTorch, регистрацию пар, holdout, staged color fit и adaptive style resolution. Проверки синтетики CPU/MPS не заменяют приёмку на реальных фотографиях, RAW-корпусе и всех выбранных ML-моделях.

### Локальное объектное хранилище

`make build-minio` собирает закреплённый исходный код MinIO по `resources/minio-source.lock.json`, проверяет Go module checksums и формирует локальный Docker image. Нельзя скачивать старый release image вместо этой сборки: бинарник заменён на release с исправлением session-policy privilege escalation. Сборка и лицензия сохранены в `.runtime/minio`; исходники — в `.cache/go-mod`. Это dev-сервис, производственная квалификация образа остаётся отдельным этапом.

На macOS `make dev-infra` создаёт ограниченный APFS sparse image `work/minio-dev.sparseimage` (2 ГБ) и монтирует его в `work/minio-volume`. Это позволяет MinIO корректно применять собственный порог свободного места на небольшом dev-томе. PostgreSQL/RabbitMQ используют прежние каталоги; существующий `work/minio` не удаляется. `make dev-infra-stop` останавливает принадлежащие проекту контейнеры; после остановки том можно отключить командой `hdiutil detach "$PWD/work/minio-volume"` из корня репозитория. Следующий запуск смонтирует его снова. Удаление sparse image уничтожает dev-объекты, поэтому автоматически оно не выполняется.

MinIO adapter проверен настоящим сервером: versioned GET/PUT, 10-МБ multipart, запрет перезаписи, гонка двух записей и удаление собственной версии при несовпадении SHA/размера. SQL attestation и HTTP загрузка/выдача артефактов ещё не связаны с этим adapter.

### Внутренний object gateway

Роль `PHOTO_ROLE=object-gateway` запускает отдельный mTLS listener (`HTTP_LISTEN`, например `127.0.0.1:8443`). Её нельзя маршрутизировать через публичный Caddy. Она соединяет credentials, GET/Range и PUT handlers с PostgreSQL и MinIO; сквозной запуск ещё ожидает проверки миграций 00020–00021 на доступной dev-БД. Политики выходов операций и cleanup/reconciliation также ещё требуют завершения.

Обязательные настройки: `DATABASE_URL` для dedicated gateway login с ролью `photo_gateway`; `GATEWAY_CLIENT_CA_FILE`, `GATEWAY_CERT_FILE`, `GATEWAY_KEY_FILE`, `GATEWAY_SIGNING_KEYS_FILE`; `GATEWAY_MAX_DOWNLOAD_BYTES` и `GATEWAY_MAX_UPLOAD_BYTES`; `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`. `S3_PUBLIC_ENDPOINT` задаёт внешний origin для presigned multipart URL; при отсутствии совпадает с `S3_ENDPOINT`. `S3_REGION` и `GATEWAY_ISSUER` опциональны. Оба endpoint S3 должны быть HTTPS; только development допускает loopback HTTP. Secret-файлы требуют0600, symlink не принимается. Формат signing keys: `{ "active": "gateway-1", "seed": "BASE64_32_BYTE_ED25519_SEED", "public": { "gateway-1": "BASE64_32_BYTE_PUBLIC_KEY" } }`. Ключи отдельные от public auth, runtime их не генерирует и не заменяет.

При старте проверяются DB-role, наличие SQL функций и уже включённое bucket versioning; bucket автоматически не создаётся. `/health/live` и `/health/ready` доступны на том же mTLS listener; ready проверяет БД и MinIO. SIGTERM отменяет активные request contexts и даёт до15s на shutdown. SQL grants, сертификаты worker registrations и output policies должны быть подготовлены до обработки задач.

### Очистка и bootstrap storage

`PHOTO_ROLE=bootstrap-storage` использует S3_* настройки и за30s создаёт отсутствующий bucket/включает versioning. БД для этой явной команды не требуется. Обычные роли gateway/maintenance только проверяют bucket, не изменяют настройки хранилища.

`PHOTO_ROLE=maintenance` использует DATABASE_URL с ролью photo_maintenance, S3_* и внутренний HTTP_LISTEN. Роль поочерёдно обслуживает cleanup output uploads, backend documents, original reservations, staging artifacts, незавершённых multipart uploads и общий artifact GC. Readiness требует последнего успешного цикла каждой очереди; `/metrics` содержит отдельные counters. Artifact GC берёт только expired exact-version object без permanent/active references, atomically отсоединяет истёкшие derivative/export/download lifecycle references, удаляет конкретную версию и отмечает `deleted` лишь после подтверждённого отсутствия объекта. Cleanup lease равен60s, S3 операция ограничена40s. Остановка отменяет активную очистку и ожидает её завершения до15s. Resumable tenant-wide destructive purge пока не подключён.

Go race test цикла и тесты конфигурации прошли. Реальный запуск maintenance/bootstrap со всеми зависимостями и миграции20–22 пока не проверены из-за недоступного Docker/PostgreSQL; эти команды ещё не являются подтверждением готовности всей системы.

### Запуск нового Python-воркера

`./scripts/run-platform.sh .venv-platform/bin/python -m photo_worker.cli run` загружает PHOTO_WORKER_ID, PHOTO_WORKER_CLASS (media, cpu или gpu), DATABASE_URL для photo_worker login, AMQP_URL, PHOTO_SCRATCH_ROOT (существующий каталог внутри рабочей области), GATEWAY_ORIGIN, GATEWAY_CA_FILE, WORKER_CERT_FILE, WORKER_KEY_FILE, WORKER_MAX_SCRATCH_BYTES, WORKER_MAX_DOWNLOAD_BYTES, WORKER_MAX_UPLOAD_BYTES. APP_ENV принимает development/production. Секреты не включаются в startup error output.

На этом этапе media исполняет asset.verify, cpu — asset.inspect, pair.register, color.stats, style.resolve, image.color, image.local, background/HDR/sensor operations, mask.rasterize, mask.transform, portrait.beauty, defects.propose, defects.repair и, при заданном PHOTO_ICC_ROOT, asset.decode. PHOTO_ICC_ROOT указывает на resources/icc этого репозитория; перед стартом проверяются хеши трёх профилей. Без этой настройки decode не объявляется установленной операцией. Перед потреблением SQL проверяет, что enabled registration/session_user и список CPU-операций соответствуют сборке; сертификат должен быть зарегистрирован, node enabled. Адресная очередь создаётся привилегированным bootstrap-queue с теми же PHOTO_WORKER_ID/PHOTO_WORKER_CLASS. SIGTERM прекращает потребление и отзывает локальный execution context. CPU heartbeat обновляет liveness каждые10s и RAM/scratch на CPU-узле; admission требует report не старше30s. Для gpu-класса обязателен PHOTO_WORKER_BACKEND=cpu/mps/cuda; всегда подключён pair.fit.color. `PHOTO_MODEL_ROOT` и `PHOTO_MODEL_LOCK` задаются только парой: при валидном lock worker добавляет соответствующие denoise/upscale/inpaint/generate/outpaint adapters, а ModelPool допускает лишь hash-verified approved weights с passed evidence выбранного backend. Нужны точная single-backend registration, certificate и operator-approved doctor report hash. CUDA требует один видимый GPU; CPU fallback gpu-класса допускается только на узле с VRAM budget=0. Полный broker→DB→gateway→MinIO→commit тест ожидает доступных dev-сервисов.

Один PHOTO_WORKER_ID соответствует одному процессу, resource class и node. SQL26 отклоняет несовместимые регистрации и отсутствующие в runtime операции. Reporter вычитает64MiB disk headroom; SQL ограничивает report бюджетом node. CPU runtime на узле с ненулевым VRAM budget обновляет только last_seen, сохраняя старые measured GPU values и timestamp: он не может сделать устаревшую GPU capacity снова актуальной. Потеря отчёта останавливает воркер, а recovery задач остаётся scheduler/reaper.

Scheduler дополнительно выполняет materialization сохранённых defects expansions
через S3 и требует настройки objectstore, как maintenance. Обработка expansion
идёт отдельно от dispatch/recovery. Repair остаётся blocked до привязки
продолжения графа; полный compiler/advance пока не завершён. Метрики:
`photo_scheduler_expansions_materialized_total`, `photo_scheduler_expansion_failures_total`.

Scheduler подключает отдельный ingest build loop: immutable snapshot → initial
verify/inspect graph → inspection outcomes → decode graph. Run leases, heartbeat
и восстановление по persisted phase ограничивают повторную работу; длительный
S3 IO не блокирует dispatch/recovery. Нужны миграции по 59 включительно.
`photo_scheduler_ingest_build_transitions_total` учитывает подтверждённые переходы,
`photo_scheduler_ingest_build_failures_total` — ошибки сборки. Создание ingest run
через API и завершение decode→catalog ещё не реализованы; SQL20–59 и этот runtime
не прошли живую сквозную проверку.

Scheduler теперь также требует `PHOTO_ICC_ROOT`, указывающий на `resources/icc`
репозитория: Go startup проверяет lock и bytes всех трёх профилей. Миграции по 68
включительно подключают decode/catalog discovery и полный coordinator originals,
adoption/rejection и завершения ingest run. Живая сквозная проверка SQL20–68 пока
не выполнена; API создания upload/ingest и cleanup staging/original reservations
ещё не завершены. Физический staging остаётся учтённым после settlement до cleanup.
