# V30 metadata adoption runtime

Статус: код написан, NOT RUN. Это установка проверки результатов; публичный non-strip admission ещё не подключён. Наличие конфигурации не означает готовность к production.

## Конфигурация scheduler

`PHOTO_DELIVERY_SCHEDULER_ENABLED=true` требуется совместно с `PHOTO_DELIVERY_METADATA_ADOPTION_ENABLED=true`. Отсутствующее второе значение либо `false` выключает отдельный adoption loop.

| Переменная | Контракт |
|---|---|
| PHOTO_DELIVERY_METADATA_ADOPTION_PYTHON | Абсолютный нормализованный путь к Python с worker dependencies |
| PHOTO_DELIVERY_METADATA_ADOPTION_WORKER_ROOT | Абсолютный корень Python package photo_worker |
| PHOTO_DELIVERY_METADATA_ADOPTION_TIMEOUT_SECONDS | Целое 1–600, default 600 |
| PHOTO_METADATA_ADOPTION_SCRATCH_ROOT | Существующий абсолютный каталог без symlink; проверяется Python перед IO |
| PHOTO_METADATA_ADOPTION_MAX_BYTES | Предел каждого encoded raster, 1–2147483648 |
| PHOTO_METADATA_ADOPTION_MAX_PIXELS | Предел каждого raster, 1–200000000 |
| PHOTO_METADATA_ADOPTION_RAM_BYTES | Не менее 268435456 + MAX_PIXELS×64 + MAX_BYTES×4 |
| PHOTO_METADATA_ADOPTION_SCRATCH_BYTES | Не менее MAX_BYTES×2 + 8388608 |
| DATABASE_URL | Доступ trusted scheduler к primary DB; значение не логировать |
| S3_ENDPOINT / S3_BUCKET | Endpoint и bucket immutable artifacts; bucket совпадает с scheduler storage |
| S3_ACCESS_KEY / S3_SECRET_KEY | Read access к точным версиям объектов; значения не логировать |
| APP_ENV | HTTP допускается только для development loopback; иначе HTTPS |

Окружение subprocess формируется из явного allowlist. Родительские PYTHONPATH, LD_PRELOAD и прочие настройки не наследуются. В stdin передаются только tenant/task/attempt/fence/result и timeout; metadata, approval, policy, credentials и адреса объектов не передаются как job authority.

Пределы RAM/scratch — консервативный admission, не измерение потребления. GPU не используется этим verifier. Последующая общая проверка Metal/MPS сохраняет согласованный GPU cap 10000000000 bytes; RTX-квалификация отложена.

## Жизненный цикл

1. При включении scheduler проверяет доступность, SECURITY DEFINER/owner и EXECUTE шести SQL entrypoints discovery/read/receipt/adopt.
2. Отдельный RunExpansionLoop выбирает одну succeeded metadata задачу после UUID cursor; отсутствие работы сбрасывает cursor. Ошибочный кандидат не блокирует последующие задачи.
3. До subprocess проверяется durable adoption receipt. Python самостоятельно загружает текущий SQL proof и повторяет authority при чтении каждого точного object version.
4. Проверяются frozen projection/Params, фактические metadata/ICC, SHA/размеры/геометрия и полная точность decoded pixels. Затем SQL атомарно принимает export/delivery item.
5. После subprocess, включая timeout/error, dispatcher независимо читает durable receipt в коротком ограниченном контексте. Ответ процесса без SQL receipt не считается успехом.
6. Shutdown отменяет job и ожидает завершения loop; stdout ограничен 4096 bytes, stderr не выводится.

Readiness включённого loop становится false при ошибке; счетчики `photo_scheduler_v30_metadata_adoptions_total` и `photo_scheduler_v30_metadata_adoption_failures_total` отражают подтверждённую обработку и ошибки loop.

## Незакрытая приёмка

Миграции 01676–01704 не применялись в рамках текущей серии. Не запускались unit/integration/native tests, реальный Python subprocess, S3/DB lifecycle, timeout/unknown commit recovery, competing schedulers и измерение ресурсов. API extraction/admission, parent preparation и SQL child admission для metadata ещё не соединены: установка runtime сама по себе не включает пользовательский режим.
