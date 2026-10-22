# PhotoEditor

Репозиторий содержит облачную платформу для фотографа, клиентский портал, агент студии и локальный пакетный редактор. Расположение кода, технологии и служебные каталоги описаны в [карте репозитория](docs/repository-layout.md). Актуальные требования и план полной реализации — [docs/implementation-v2](docs/implementation-v2); состояние приёмки — [текущий реестр](docs/implementation-v2/audit/mac-deep-testing-2026-10-17/current-status.md).

Ниже описан запуск исходного локального редактора.

Локальный пакетный редактор портретов для macOS / Apple Silicon. Он приводит серию к общей яркости, корректирует цвет и локальный свет, ретуширует кожу и выполняет ограниченную коррекцию контуров лица и тела. Оригинальное разрешение обрабатывается тайлами, а MediaPipe используется для поиска лиц и построения масок кожи.

Текущая версия — **0.3.3**, категория пайплайна — **«улучшение качества»**. После детерминированной обработки blur-gate направляет только нерезкие лица в CodeFormer, а ArcFace проверяет сохранение личности. Финальный полноразмерный проход выравнивает баланс белого, сжимает опасные света, приглушает остаточные блики и восстанавливает естественный цвет кожи на всех найденных лицах, включая восстановленные моделью.

## Структура приложений

- `apps/local-editor` — исходный локальный пакетный редактор: Python CLI и настольный PySide6 интерфейс. Код обработки, собственный `pyproject.toml` и закреплённые зависимости находятся здесь. Корневые `run.sh` и `run-e2e.sh` сохраняют прежний запуск.
- `apps/photographer` — рабочее приложение фотографа на Expo / React Native; интерфейс на React Native Paper (Material Design 3), веб и мобильные цели.
- `apps/portal` — клиентский портал на Next.js для просмотра и получения фотографий.
- `apps/studio-agent` — локальный Python-агент студии для обмена файлами; отдельное приложение от настольного редактора.
- `backend` — сервер Go; `workers` — Python обработчики; `packages` — общие клиентские контракты; `docs/implementation-v2` — план и доказательства текущей реализации.

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

Для native MinIO-тестов на почти заполненном Mac можно использовать отдельный ограниченный APFS-том внутри репозитория. Это сохраняет штатную защиту MinIO от переполнения:

```bash
python3 scripts/dev-minio-volume.py --name minio-integration-20261018
export PHOTO_TEST_NATIVE_MINIO_DATA_ROOT="$PWD/work/minio-integration-20261018-volume"
```

Том разреженный, максимум 2 ГБ; тесты создают в нём только собственные временные каталоги и удаляют их после остановки своего сервера. Fixture отклоняет путь вне `work`, symlink и обычный немонтированный каталог. Старые образы не перезаписываются. После завершения всех тестов и остановки native MinIO том можно отключить: `hdiutil detach "$PHOTO_TEST_NATIVE_MINIO_DATA_ROOT"`. Это средство локального тестирования, а не production-квота или аппаратная квалификация.

Исполняемая single-node production-топология находится в `deploy/compose.yaml`; подготовка секретов, обязательные immutable image digests, build arguments и профили `mail`/`cuda` описаны в `deploy/README.md`. Compose оставляет наружу только Caddy 80/443, запускает migrate/storage/queue bootstrap как one-shot barriers, использует внутреннюю сеть для DB/Rabbit/MinIO/gateway и включает healthchecks для API и фоновых ролей. `make deploy-check` проверяет состав ролей, nonroot/read-only ограничения, mTLS worker wiring, distinct worker IDs, profile isolation и отсутствие mutable/default-secret образов. Реальный production build, TLS authority, restore drill и аппаратные benchmarks остаются внешними release gates.

Go entrypoint `backend/cmd/photo` поддерживает роли `migrate`, `bootstrap-queue`, `api`, `publisher`, `dead-letter-worker`, `mail-worker`, `collection-worker`, `scheduler`, `object-gateway`, `maintenance`, `bootstrap-storage` через `PHOTO_ROLE`. Конфигурация и credentials задаются явно: `DATABASE_URL` с отдельной ролью сервиса, для очередей `AMQP_URL`; API требует отдельные auth credentials/keys. Scheduler выполняет resource admission, отправку ready-задач через outbox и recovery истёкших аренд. Collection worker последовательно потребляет durable `collection.mutate` commands из `q.worker.collection`; command связан с сохранённым event fence, поэтому повторная доставка не применяет chunk повторно. Для dispatch нужны заранее разрешённые node/worker, свежие capacity reports и квалификация конкретной пары operation/backend. Компиляция/advance графов, object gateway, полные API/клиенты и production wiring всех операций ещё не завершены.

Новый численный тракт включает рабочие ICC, tiled decode, цветовые преобразования NumPy/PyTorch, регистрацию пар, holdout, staged color fit и adaptive style resolution. Проверки синтетики CPU/MPS не заменяют приёмку на реальных фотографиях, RAW-корпусе и всех выбранных ML-моделях.

Текущая проверка на Mac (2026-10-17): локальный CPU-контур обработал шесть реальных фотографий с десятью лицами без ошибок; техническая проверка выходов прошла. CodeFormer/ArcFace действительно запущены на Metal без искусственного лимита памяти. На этих примерах обнаружены повторные детекции и сгенерированные детали глаз/зубов, проходящие численный identity-gate; результат сохраняет `NEEDS_REVIEW` для решения фотографа и не получает автоматического визуального одобрения. Это не квалификация новых worker ModelLock или RTX. Подробные результаты и ограничения: [реальные фотографии и Metal](docs/implementation-v2/audit/mac-deep-testing-2026-10-17/real-photo-quality-results.md).


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

## Apple Silicon: Metal, замены и временно недоступные функции

На macOS сборка OpenCV с GCD может игнорировать положительный предел CPU-потоков. Worker в этом случае явно включает последовательную обработку OpenCV и проверяет фактическое число потоков; функции не отключаются. Настройки возвращаются при выходе из задания, включая ошибку и вложенные ограничения. Если даже последовательный режим не соблюдает предел, запуск вычисления отклоняется.

Политика разработки: сначала использовать совместимый Metal/MPS adapter; если его нет — реализовать подходящий альтернативный backend (Core ML либо явно зарегистрированный CPU worker). Отключать функцию только при отсутствии установленной совместимой реализации. Замена обязана сохранять контракт результата и качество; наличие альтернативы в библиотеке само по себе не означает её подключение в PhotoEditor. Не допускается скрытый CPU fallback под регистрацией MPS. Metal не имеет фиксированного ограничения 10 ГБ; CUDA MVP ограничен 10 000 000 000 байт.

Состояние по статическому чтению исходников 2026-10-17; это не результат runtime-квалификации.

В коде добавлен пооперационный выбор установленного `semantic.preview_background_cleanup`: совместимый MPS adapter сохраняется при passed evidence Big LaMa, approved model lock и установленной factory; явно unsupported MPS-модель исключает только эту preview operation. Missing/untested evidence остаётся ошибкой конфигурации. Перед регистрацией проверяются pinned файлы модели. Это изменение кода не является результатом запуска или квалификации на Mac; Region теперь принимает квалифицированный MPS provider с отдельными runtime/model/device/calibration pins; полный transfer/learning пока сохраняют свои ограничения.

| Возможность | Текущая реализация на Mac / замена | Что ещё требуется |
|---|---|---|
| Маска человека, лица/landmarks, поза, DINOv2, SegFormer, BiSeNet, MobileSAM, depth | В MODEL_BACKENDS предусмотрен MPS для соответствующих PyTorch моделей | Установленные веса, adapter и passed backend evidence; не отключать только из-за отсутствия CUDA |
| SCUNet denoise, RealESRGAN 2x/4x, Big LaMa | Предусмотрен MPS для тех же моделей; это смена backend, а не замена эффекта | Квалификация конкретного model/runtime lock на Mac |
| SD1.5 inpaint, generation/outpaint | Предусмотрен MPS; Diffusers поддерживает Apple Silicon | Проверить конкретный pipeline, память, precision и качество; Core ML — возможная будущая альтернатива, пока не подключена |
| U2Net | Текущий adapter CPU-only | Явно зарегистрированный и квалифицированный CPU worker на Mac; не рекламировать как Metal |
| identity512, eye_open, glare_mask, reflection_rgb | Текущие ONNX adapters объявляют CPU/CUDA, MPS отсутствует | Исследовать ONNX Runtime CoreMLExecutionProvider; пока Core ML adapter не реализован. CPU требует собственной квалификации |
| V28 reflection | Прямой MPS installer возвращает None; существует явный CPU/CUDA ONNX provider | На Mac использовать отдельный qualified CPU worker с reflection_rgb и CPU runtime lock; Core ML пока не реализован |
| Region preview | MPS provider сохраняется и регистрируется как GPU; требуется точное совпадение backend и самостоятельная измеренная calibration | Не заменяется скрытым CPU. Без установленного полного provider/approved Mac evidence остаётся unavailable; CUDA calibration не используется для MPS |
| Semantic / semantic artifact / learning providers | Пока опускаются в MPS worker | Отдельные последующие порты; независимые совместимые previews сохраняются |

Подробный план на уровне функций, моделей и SQL admission: [Metal alternatives implementation](docs/implementation-v2/metal-alternatives-implementation.md). Уже существующие MPS removal/background cleanup previews и общий render следует сохранять независимо от CUDA-only полного semantic.transfer. Learning дополнительно блокируется ONNX identity512 и CUDA-only HoldoutDepthProvider; смены общего selector недостаточно. Статусы `implemented/unqualified` и `planned` в плане не являются подтверждением готовности к выпуску.

Порядок доработки: составить пооперационную матрицу зависимостей этих providers → выбрать уже имеющиеся MPS модели → подключить недостающие MPS/CPU/Core ML adapters → зарегистрировать точный backend/runtime/model lock → выполнить глубокую проверку качества в запланированной фазе тестирования. Недоступность одного adapter не должна выключать остальные возможности worker. До реализации и квалификации замены UI показывает фактическую доступность; таблица не обещает уже работающие замены.

Основания для альтернатив: [PyTorch MPS](https://docs.pytorch.org/docs/main/notes/mps.html), [Diffusers MPS](https://huggingface.co/docs/diffusers/optimization/mps), [Diffusers Core ML](https://github.com/huggingface/diffusers/blob/main/docs/source/en/optimization/coreml.md). Матрица проекта: workers/photo_worker/models/backend_support.py; ограничения installers: workers/photo_worker/runtime/service.py и reflection_config.py.

### Hair / clothing: совместимая CPU реализация

Локальная коррекция волос и одежды имеет явный CPU adapter, который можно установить на Mac отдельным CPU worker; Metal worker не должен выдавать его за MPS. Worker требует `PHOTO_LOCAL_PORTRAIT_ENABLED=true`, `PHOTO_LOCAL_PORTRAIT_RUNTIME_LOCK` и `PHOTO_LOCAL_PORTRAIT_RUNTIME_SHA`, resource class/backend `cpu`. API требует `PHOTO_V28_LOCAL_PORTRAIT_CONFIG` и отдельный scheduler connection `V28_LOCAL_PORTRAIT_SCHEDULER_DATABASE_URL`; startup проверяет SQL installation, включая run-history discovery 02487. Runtime lock и current qualification остаются обязательными. Код и сценарии написаны без исполнения; это не подтверждение качества конкретной установки.

### Glare: реконструкция без донора

В исходниках добавлен отдельный source-only режим Glare с текущим согласием исходного человека и обязательным решением фотографа. API включает его только при `generative_enabled: true` у проекта в `PHOTO_V28_GLARE_DONOR_CONFIG` и текущем qualified restoration profile. Worker требует `PHOTO_GLARE_GENERATIVE_ENABLED=true`, approved/passed CPU Big-LaMa из того же model lock, pinned torch и обновлённый restoration runtime hash. API и worker должны использовать согласованные qualified pins; отсутствие установленной совместимой реализации не даёт разрешения на новую задачу.

Это явный CPU worker path, доступный и на Mac при собственной квалификации CPU. Прямой Metal/MPS Glare adapter пока не подключён. Существующий Big-LaMa helper используется повторно; генеративный результат сохраняет review barrier и не заменяет исходную фотографию автоматически. Изменения написаны без запуска миграций, тестов и квалификации.

### Editor: обязательная калибровка финального рендера

В deployment с включённым Editor задайте `PHOTO_RENDER_CALIBRATION_FILE` и `PHOTO_RENDER_CALIBRATION_SHA256` для API и scheduler. Файл — закреплённый оператором каталог измеренных render profiles, соответствующих текущим model/runtime/backend/device/dataset и ICC. Scheduler отказывается запускаться без обоих параметров или при неверном файле/хеше; API не принимает новые полные рецепты без загруженного каталога. Наличие файла не заменяет квалификацию конкретного профиля.

Полный рецепт проходит authored layers → Apply effects/review → независимый seal → final V2compose. Статус завершённого Apply не означает готовое финальное фото. Код и сценарии этой цепочки пока не квалифицированы исполнением.

### Download volume limits

Scheduler accepts `PHOTO_DOWNLOAD_VOLUME_BYTES` (default10737418240, range1..10737418240) and `PHOTO_DOWNLOAD_VOLUME_FILES` (default5000, range1..5000). Lower limits split volumes without omitting selected files; one oversized file is delivered separately. Invalid values stop scheduler startup. These operational limits do not enlarge the versioned wire/storage ceilings. Download retention remains24h and signed links60s; configurable TTL wiring is still pending.

Download lifetime configuration: `PHOTO_DOWNLOAD_RETENTION_SECONDS` defaults to `86400` (24 hours), range `1..86400`; `PHOTO_DOWNLOAD_LINK_TTL_SECONDS` defaults to `60`, range `1..60`. Invalid values stop API startup. New owner and guest archive jobs freeze both settings in their run configuration; changing ENV affects subsequent jobs only. Existing jobs without the policy retain the historical defaults. Volume intents use their job's frozen link policy; direct guest item intents freeze the current link policy at creation. Signing repeats current scope/object authority and uses the consumed intent's immutable TTL, bounded by remaining artifact lifetime. URLs and intents retain the existing maximum 60-second security cap, and client bounds remain compatible with reductions. Retention begins only at atomic archive finalization; no source/photo retention policy changes.


### Reflection на Mac: явная CPU-альтернатива

`reflection_installation_from_environment` и `ReflectionModelAdmission` уже поддерживают backend `cpu`. Для этой операции существующий контракт использует resource class `gpu` даже при CPU backend: запускать отдельный worker с `resource_class=gpu`, `backend=cpu`, а не регистрировать CPU provider внутри Metal worker. Нужны `PHOTO_REFLECTION_ENABLED=true`, `PHOTO_REFLECTION_MODEL_LOCK_SHA`, `PHOTO_REFLECTION_RUNTIME_LOCK` (абсолютный путь), `PHOTO_REFLECTION_RUNTIME_SHA`, установленный approved/passed CPU `reflection_rgb` и соответствующая текущая qualification. CPU runtime lock фиксирует `onnxruntime`, платформу и байты компонентов; CUDA evidence не переиспользуется.

На MPS worker Reflection не регистрируется; при установленном квалифицированном CPU worker система может выполнить ту же операцию через его явный профиль. Это описание текущего пути в коде, не подтверждение квалификации конкретного Mac. Прямой Core ML/MPS adapter остаётся будущей заменой.

Region MPS: backend-aware admission/provider/profile код и forward-only migration `02490` написаны без выполнения проверок. SQL сохраняет disabled-by-default catalog, approved policy/report/license/dataset и exact backend pins. MPS требует собственного device calibration catalog; смена backend не меняет старые retry/results. Production deployment должен установить полный квалифицированный Region renderer; наличие исходников не подтверждает установку/качество на Mac. Модули рецепта вне `supported_modules` отклоняются целиком, без молчаливого пропуска.

Region native intermediate: `install_native_region_color` монтирует full-canvas color/strength; optional passed LRASPP ModelPool добавляет adaptive/local, optional passed SFD/FAN/Face19 pool добавляет свежий анализ и прежнюю beauty обработку. Все модели вызываются на выбранном CUDA/MPS backend. Подключены body с общим premultiplied RGB/alpha warp, guided person-mask transform, crop по каноническому CompositionPlan, installed passed Real-ESRGAN x2/x4, SCUNet denoise, portrait_detail, regional adjustments/series и background transparent/solid/blur/asset с typed portable asset/decode provenance. Conservative dust выполняется между global color и local; conservative defects после portrait_detail. Staged preview измеряет task-private pixel lineage; свежие defects заново вычисляют DoG/Face19 и fixed-threshold eligibility с сохранением permanent protection. Обычный exact replay сохраняет проверку исходных RGB/alpha. Strength применяется один раз после local. External LUT dependencies физически проверяются. Installed passed Big-LaMa и SD1.5 pools добавляют inpainting, два отдельных seeded generation этапа и outpaint; отсутствующая квалифицированная модель явно недоступна. Сервер различает background/generate по actual Apply DAG и разрешает outpaint только с единственным каноническим composition.plan prerequisite того же run. Go Freeze и adopter связывают actual stage receipts/Params/report; клиент показывает ROI на фактическом целевом холсте. Reviewed repair связывает исходное решение отдельно со свежими spatial repair pixels; culling сохраняет текущий выбранный item, masking требует установленных qualified authoring providers. Полный SQL lifecycle/concurrency охват и независимый обзор свежего replay ещё не завершены. M2 остаётся открытым; capability не активируется до завершения цепочки и квалификации. Написанные сценарии не запускались.

Region native startup (CUDA/Metal): set `PHOTO_REGION_RUNTIME_ENABLED=true`,
`PHOTO_REGION_INSTALLATION_FILE` to an absolute bounded JSON file and
`PHOTO_REGION_INSTALLATION_SHA` to its externally pinned SHA256. The closed
configuration contains `qualification` (runtime/model/ICC/device/dataset/policy
hashes and backend), `runtime_path`, `calibration_path`, `calibration_sha`.
The worker constructs native providers from its installed qualified model pool;
configuration cannot name executable factories. SQL admission still verifies
current task-scoped qualification independently. Current production assembly supports the installed modules listed above; missing
qualified native providers fail explicitly and are never silently skipped.
Independent Region adoption additionally requires `PHOTO_REGION_ADOPTER_ENABLED=true`
and `PHOTO_REGION_ADOPTER_BACKEND=cuda|mps`, the same pinned Region installation,
`PHOTO_MODEL_ROOT`, `PHOTO_MODEL_LOCK`, `PHOTO_ICC_ROOT`, and bounded
`PHOTO_ADOPTER_LAYER_SCRATCH_ROOT`/`PHOTO_ADOPTER_LAYER_SCRATCH_BYTES`.
The adopter builds its own installed ModelPool, re-executes the complete recipe
from declared inputs, repeats actual geometry and ICC presentation, and compares
exact derivative SHA/bytes/projection/cache key. Submitted output pixels are not
used as reconstruction inputs. Missing replay installation refuses Region
adoption; a CUDA qualification never authorizes Metal. Source code and authored
scenarios do not establish deterministic model qualification on either backend.
This intermediate assembly does not mark the whole Metal Region module complete.

Reflection после решения фотографа сохраняет точный выбранный WorkingImage через `POST /reviews/{id}/reflection-apply`: accept создаёт одну детерминированную версию с исходной lineage, reject открывает исходную версию, skip не создаёт версию. Повторная обработка изображения при сохранении не запускается. Текущая квалификация CPU/CUDA, доступ к исходнику и происхождение ручных масок перепроверяются при чтении/просмотре, решении и сохранении. Это реализация контуров; выполнение миграции 02494, физические сценарии и квалификация на фотографиях ещё не подтверждены. Подробности: [Reflection source closure](docs/implementation-v2/audit/reflection-current-source-closure-2026-10-17.md).

Semantic transfer: экран обработанных кадров использует `GET /semantic-targets/{target}/preview` (02498), а не первый derivative общего child run. Сервер связывает target с canonical shown-copy, точным preview producer/fence и PNG; проверяет WorkingImage/pixels metadata parity, физические PNG bytes/ICC и повторяет текущий доступ после IO/подписи. Отдельный POST `/version` сохраняет тот же выбранный результат без повторного рендера. Metal-замены остаются пооперационными: этот read/copy путь не требует CUDA, полный semantic.transfer по-прежнему требует установленного квалифицированного CUDA adapter. Код и сценарии написаны без исполнения; качество установки не квалифицировано.

Backup isolated acceptance is installed through the staged, externally pinned `restore-accept-operator/v1` configuration and four concrete probe installations. It uses the normal main-thread worker with a shared in-process running-revoke barrier, current SQL render qualification, independent deletion/consent/key authority, and internal service endpoint checks. Recovery/replay/probe pins must be independently reviewed; no acceptance report authorizes production writers. The source contract, configuration shape and deferred physical checks are documented in [Backup acceptance source map](docs/implementation-v2/audit/backup-accept-current-source-map-2026-10-17.md). This path is source-authored and has not been executed or qualified.

API readiness now checks the API database, separate authentication database and versioned object storage under one three-second request deadline. `/health/live` remains process liveness. Optional AI worker loss disables only the affected operations; installed, independently qualified CPU/Metal alternatives remain eligible. Unknown backend evidence is refused per operation, and the editor displays localized availability messages without internal reason codes. These changes and scenarios are source-authored, not executed qualification.

### Lens correction (V25)

Collection Lens correction uses qualified immutable camera/lens profiles, one
profile and strength per selected photograph, a frozen full selection, and a
recoverable request journal. Each result has a version-bound before image and a
verified rendered after image. Applying creates an accepted immutable version
and selects the same collection occurrence by compare-and-swap; originals remain
available. Geometry reset and uncovered edges require photographer acceptance.

The API registry is configured with `PHOTO_V25_LENS_REGISTRY`; the Lens worker
uses `PHOTO_LENS_REGISTRY`. Deploy matching qualified coefficient/qualification
pins. Catalogue registration indexes existing physically verified artifacts; it
never qualifies a profile or invents an available model. Enable the adoption
worker's actual Lens preview path with
`PHOTO_ADOPTER_V25_LENS_PREVIEW_ENABLED=true` and the existing qualified working
and target ICC settings. Missing preview deployment leaves Apply unavailable in
the UI until a verified after image can be displayed.

Lens geometry and its shared native colour preview pipeline do not require CUDA;
they remain available with the CPU/Metal deployment. No CUDA-only replacement is
claimed for this module. An unqualified/missing lens profile stays unavailable
rather than being silently replaced by unrelated coefficients.

Opt in to Lens deployment using the existing V2 services and the dedicated
overlay:

```sh
docker compose -f deploy/compose.yaml -f deploy/compose.lens.yaml --profile v2 up -d
```

This command is an operator instruction; it has not been executed as part of
coding. Provide the base deployment variables and all required Lens inputs:

- `PHOTO_LENS_REGISTRY_HOST_FILE` and `PHOTO_LENS_REGISTRY_SHA256`: existing
  regular operator-qualified registry file and its exact raw-byte SHA. The API
  and CPU worker mount identical read-only bytes, including accepted runtime,
  coefficient, qualification and photographic dataset pins.
- `PHOTO_LENS_ICC_HOST_DIR`: existing accepted ICC bundle/manifest directory.
  API, scheduler, CPU worker and adopter mount the same read-only bundle.
  The bundle must include `work_rec2020_gamma24_v1`, `srgb_v1` and
  `linear_srgb_v1`; the existing ICC registry verifies the files and derives
  exact working/target hashes. Native preview qualification must match them.
- `PHOTO_LENS_POLICY_SHA256`, `PHOTO_LENS_RAM_BYTES`,
  `PHOTO_LENS_SCRATCH_BYTES`, `PHOTO_LENS_OUTPUT_BYTES`: accepted deployment
  capacity policy and positive bounded resource limits. These are operator
  limits, not measurements inferred from a host model name.
- `PHOTO_LENS_API_DATABASE_URL`, `PHOTO_LENS_SCHEDULER_DATABASE_URL`,
  `PHOTO_LENS_WORKER_DATABASE_URL`: separate least-privileged runtime logins
  authorized only for `photo_api`, `photo_scheduler`, `photo_worker`
  respectively. Keep the base auth DSN restricted to `photo_auth`. Migration or
  administrator logins are rejected by runtime role guards; do not reuse them.

No registry, coefficient, qualification report, photographed dataset or ICC
qualification is synthesized by the overlay. Missing or mismatched inputs fail
startup. Object-store credentials and gateway mTLS retain their existing scoped
base deployment configuration. Lens stays on its qualified CPU implementation
on both CUDA servers and Mac/Metal hosts; the overlay does not require a GPU.


Internal Editor CPU installation: after migration02547, CPU workers require the installed Editor origin/adoption SQL contracts before advertising `editor.effects_seal`; `editor.layers_precompose` additionally requires an installed CPU native renderer. Start the actual runtime and its qualified registration/heartbeat first, then use the maintenance-only `photo.configure_editor_internal_dispatch(tenant_id,worker_id,operation,true)` for each operation. The function checks current installed authority, exact CPU qualification and worker/node freshness. Startup installation checks do not require activation, so this order has no circular dependency. This does not qualify image quality; source/receipt/independent adoption gates still apply.

Capability discovery for new Collection Apply, style analysis, full editor recipes and calibrated Capture creation is server-derived. It follows the actual combined selectors and immutable policy/recipe inputs. Pending saved requests retain their original identities and recover even when a worker is temporarily unavailable. Editor advisory CPU readiness requires migration 02548 and current enabled, qualified workers/nodes with matching capacity-policy heartbeats; it does not activate a worker or substitute for runtime qualification. Final rendering still validates its actual measured resources after effect outputs exist. These additions have authored tests; execution and deep testing remain the next phase.

Owner app private links: configure `EXPO_PUBLIC_PORTAL_ORIGIN=https://your-portal.example` when building the photographer app. Use the HTTPS root origin without path, query, credentials or fragment. The owner explicitly copies/shares the one-time private URL; its secret stays in the URL fragment and is never saved in the creation journal. Backend `PORTAL_ORIGIN` remains a separate server setting.


API admission database credentials: set `PHOTO_ADMISSION_SCHEDULER_DATABASE_URL` to a separate least-privileged login with membership in `photo_scheduler` for V22 analysis and V26 render/Region admission pools. The main `DATABASE_URL` continues to connect as `photo_api`/`photo_portal`; it need not receive scheduler membership. An unset admission URL retains the existing multi-role deployment fallback. `AUTH_DATABASE_URL` and the explicit optional `V23_TRAINING_SCHEDULER_DATABASE_URL` remain separate. Compose forwards the new setting. Do not use a database owner or superuser for these runtime logins.

V22 analysis no longer applies the old fixed 10 GB Metal admission ceiling. Actual compiled workload estimates still pass node/worker capacity and residency checks; an explicit `PHOTO_V22_VRAM_BYTES` deployment budget is respected. The default CUDA admission ceiling is decimal 10 GB for the RTX MVP. CPU analysis reserves no GPU memory.


Хранилище и браузерный CORS: `S3_CORS_STRATEGY` по умолчанию равен `bucket`: bootstrap устанавливает и считывает точное bucket-правило GET/HEAD/PUT с ETag. Для закреплённого MinIO, который отвечает 501 на bucket CORS API, Compose явно выбирает `minio-server` только у `bootstrap-storage` и передаёт серверу точные `APP_ORIGIN,PORTAL_ORIGIN` через `MINIO_API_CORS_ALLOW_ORIGIN`. Эта ветка проверяет полный действующий список через административный readback с учётом environment overrides, затем реальные OPTIONS публичного S3 endpoint для каждого origin и чужого origin. Wildcard, лишний/пропавший адрес, недоступный admin API и несовпадающая активная политика останавливают bootstrap; автоматического fallback нет. Bootstrap credentials должны иметь доступ к чтению конфигурации MinIO (`admin:ConfigUpdate`, как требует закреплённый API); новые права runtime-пользователям не выдаются. Server CORS действует на весь MinIO и допускает более широкий набор методов для доверенных origins; конкретную операцию по-прежнему ограничивает S3-подпись. Проверки подтвердили отказ DELETE по GET/PUT-подписи. Для изменения origins требуется изменить конфигурацию MinIO и повторить bootstrap. Проверка bucket-CORS rotation требует провайдера, реализующего этот API; native MinIO её не квалифицирует.
