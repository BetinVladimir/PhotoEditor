# Детерминированное исполнение native RAW

Исправление ограничивает native OpenMP внутри `decode_raw` одним потоком. Оно не меняет AHD/DCB, camera/daylight/auto WB, exposure, highlight policy, linear sRGB RGB16, ICC transform, tile size или выходной float32 контракт. Для real Fujifilm RAF отдельно наблюдалось различие native RGB16 и working RGB при нескольких OpenMP threads; поэтому совпадение формата, dimensions и params недостаточно для принятия разных SHA.

## Реализация

`workers/photo_worker/operations/asset/decode.py` использует отдельный `deterministic_raw_threads`, а не `worker_thread_budget(1)`: второй имеет main-thread-only контракт, тогда как RAW utility не должна добавлять новое ограничение вызывающему коду.

Scope сериализован process-local RLock. До загрузки rawpy сохраняются фактические counts уже загруженных OpenMP pools. OMP_NUM_THREADS временно устанавливается в1, затем rawpy загружается и создаётся **новый** ThreadpoolController: controller, созданный до import, не обнаруживает поздно загруженную private LibRaw runtime. Через native API все обнаруженные OpenMP pools ограничиваются1 и проверяются до imread/postprocess. OPENMP-enabled build без контролируемого pool отклоняется; serial build с OPENMP=false не требует несуществующего pool. Наличие environment variable само по себе не считается доказательством ограничения.

Весь imread/postprocess и проверки native RGB16 исполняются внутри scope. В finally обновлённый controller восстанавливает каждую ранее существовавшую runtime к её **точному фактически наблюдённому** count. Новый pool, которого до import не существовало, не имеет предыдущего native state: он принимает scalar positive OMP_NUM_THREADS внешнего scope, если значение помещается в signed32-bit integer; иначе остаётся conservative1. Для unset, auto, nested-list и неподдерживаемых форм environment восстанавливается дословно, но новый pool не объявляется восстановленным к неизвестному числу. Например, внешний OMP2 возвращает новый LibRaw pool к2; OMP unset/auto оставляет новый pool1. Проверка восстановления обязательна; ошибка восстановления не выдаётся за успех.

Эти гарантии относятся к последовательной heavy-task модели worker. RLock предотвращает пересечение RAW scopes одного процесса, но не превращает global native controls и environment в изолированные настройки произвольного параллельного приложения. Функция поддерживает background caller; искусственного main-thread запрета нет.

`RAWThreadControlError` отделяет ошибку установленного native runtime от плохой фотографии. `asset.adapters._decode` преобразует её в `CONFIG_INVALID`; обычные RAW/ICC input ошибки сохраняют прежнюю классификацию. Ошибка/отмена освобождает rawpy handle и восстанавливает native limits/environment; готовые outputs не фабрикуются.

## Тесты и граница доказательства

`tests/platform/test_raw_thread_execution.py` проверяет actual-controller вызовы на управляемых native fixtures: enforcement, поздний import, preexisting pools с разными counts, новый pool при outer2/unset/auto/list, частичный import failure, отсутствующий/unapplied control, decode failure, cancel, restoration failure, serial build, background caller и сериализацию concurrent RAW calls. Проверяется сохранение прежних options и `CONFIG_INVALID` при installation failure.

Локальный targeted suite:95 PASS, лог `work/raw-thread-determinism-fix-r1/tests-r3.log`; вместе с существующими RAWDevelopment, RGB16 layout и worker_thread_budget tests. Это доказательство control logic и регрессии wiring, не подтверждение итоговых пикселей native LibRaw на RTX или качество реальных фотографий.

Перед actual adoption/deployment требуются cold/warm повторы на итоговом pinned runtime: шесть настоящих camera formats и RAF с внешними thread conditions1/2/worker2; наблюдение effective pools внутри декодирования и после него; сравнение native16/NPY pixels и рабочих outputs; явное сохранение параметров и ICC; RAM/scratch/OOM checks. Исходные различающиеся immutable outputs не перезаписываются, task/catalog states не сбрасываются вручную. Старые synthetic/physical receipts не считаются доказательством новой сборки.

Для физического кандидата `work/raw-thread-determinism-fix-r1/candidate-overlay` подготовлен из прочитанного a302 source с двумя reviewed production files; base source, SHA и unified patches сохранены. Повторно вычислить весь runtime lock нового кандидата и не использовать прежний a302 pin для изменённой сборки. Исходные рабочие копии, модели, сервисы и БД этим документом не изменяются.
