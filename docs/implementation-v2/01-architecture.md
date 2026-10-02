# Архитектура, совместимость и ресурсные границы

## Решения

Расширяем один monorepo. Go остаётся владельцем авторизации, транзакций, quotas, состояния задач и публикации. Python выполняет вычисления через существующий task runtime. Expo — редактор; Next — клиентский portal. PostgreSQL — источник состояния, RabbitMQ — доставка, MinIO — versioned artifacts. Новый distributed framework и отдельная vector DB не вводятся.

Рабочий pixel contract остаётся существующим WorkingImage. Не переопределять transfer function рабочего пространства по названию ICC: использовать ICCRegistry и ColorSpace из текущего кода. ML-adapter явно преобразует рабочий RGB в ожидаемый tensor domain и возвращает корректировку через согласованный color transform. Alpha straight; геометрия через TransformChain; оригиналы неизменяемы.

## Новые модули

- backend/internal/sessionbrain: immutable анализ серии и планы согласованности.
- backend/internal/learning: события коррекций, snapshots обучения, promotion.
- backend/internal/capture: сессии ingest от студийного агента и предупреждения.
- backend/internal/semanticedit: намерения, планы переноса, review.
- backend/internal/provenance: экспорт истории и C2PA signing jobs.
- backend/internal/delivery: замороженные пакеты выдачи и атомарная публикация.
- backend/internal/interchange: импорт/экспорт sidecar bundles.
- workers/photo_worker/operations/session, learning, capture, identity, semantic, provenance, optical.
- apps/studio-agent: локальный Python daemon для watch-folder; camera adapters как отдельные явно включаемые плагины.

Эти модули используют существующие runs/tasks/reviews/artifacts, без параллельного job engine. Для всех v2 orchestration runs вводится ровно один kind=v2. Тип алгоритма хранится в v2_workflows.workflow_type. runs.status — единственный статус исполнения. Компилятор и финализатор определены в 09-workflow-transactions.md; дочерние Apply/export сохраняют прежние kind и контракты.

## Конвейер v2

Freeze selection → qualify capabilities → reserve quota → persist workflow+outbox → analyze proxies → immutable plan → human approval если нужен → compile Apply v4 с source-bound overrides → quality+identity → adoption → delivery.

План не содержит исполняемый код, SQL или URL загрузки. Только operation ID/version, ArtifactRef, typed params и target IDs. Worker получает signed access только к declared inputs. Никаких скачиваний моделей по prompt/task params.

## Совместимость

Не менять существующий StyleV2 молча. Ввести EditingRecipeV3, который содержит base_style, session_corrections и ordered override refs. TranslateRecipeV3 переводит допустимые модули в существующий effective style; новый модуль получает новую operation version и renderer_version. В старом Apply остаётся прежнее поведение. Старые runs завершаются старым compiler.

Cache key = SHA256(JCS(operation,version,ordered inputs,params SHA,renderer version,model lock,precision,ICC locks)). Tenant включается в namespace cache. Изменение backend precision инвалидирует cache. При reuse повторно проверить lifecycle и действующие права.

## Ошибки и retries

INPUT_INVALID/UNSUPPORTED_FEATURE/CONFLICT/UNQUALIFIED — без автоматического повтора. Dependency failure — существующий bounded retry. CUDA OOM: освободить модель, уменьшить tile один раз по deterministic fallback plan, затем явный failure. Новый plan hash и execution profile фиксируются; нельзя выдавать fallback за прежний execution. Cancellation проверять между tiles, decode frames и model calls; существующий fencing запрещает late commit.

## RTX 3060 / Ryzen 5600 / RAM 32 GB

Нормативные начальные budgets, concurrency и этапы роста: [12-scale-product.md](12-scale-product.md). Не дублировать значения в deploy config без contract test соответствия этому профилю.

Не допускать независимые ModelPool в нескольких GPU-процессах без node-wide resource reservation. Текущий pool уже удерживает одну borrowed model; сохранить его mutex и qualification checks. Preview получает приоритет на границе задач/tiles; не прерывать произвольный CUDA kernel. Для переключения сохранить checkpoint и освободить slot по fence.

Full RGB float32 = width*height*12 bytes. При 45 MP один буфер ~540 MB. EstimateMemory учитывает одновременно source/output, masks, temporary arrays, encoder и модель; mmap не считается нулевым RSS. Tile candidates [768,512,256], halo определяется операцией. Cap input 200 MP и output 200 MP до выделения памяти; 4x upscale означает 16x pixels.

Scratch резервируется до decode. Расчёт NPY+temporary encoded bytes+margin, disk watermark не ниже 15%. Cache eviction только unreferenced artifacts. Capacity heartbeat каждые 10 s, просрочка 30 s запрещает dispatch. Нагрузочный тест смешивает API, ingest, preview, Apply и ZIP.
