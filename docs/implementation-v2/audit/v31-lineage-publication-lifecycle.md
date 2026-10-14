# V31: жизненный цикл публикации lineage

Статус: код и тестовые сценарии написаны; миграция 01390 не применена, тесты не запускались. Это описание реализации, не evidence успешной приёмки.

`CompiledExportLineageSource` получает канонический граф выполнения, компилирует `SigningLineage`, перепроверяет граф до и после публикации. `ImmutableSigningLineagePublisher` выполняет reserve → immutable PUT/recovery → complete. `SQLSigningLineageStore` использует назначение `v31_signing_lineage`, owner=export, run=канонический run экспорта. ID артефакта определяется UUIDv5(export, "signing:lineage:" + SHA256).

SQL сравнивает точные project/version/task/run/artifact/SHA/key/object_version/bytes/MIME источника; проверяет актуальный ready export, canonical task result и attempt/fence, действующую сессию и права редактора, разрешение скачивания. Публикация lineage сама по себе не доказывает семантику операций и не разрешает подпись.

| Состояние | Повтор reserve | Complete |
|---|---|---|
| Отсутствует | Резервирует квоту, создаёт pending | Требует действующий токен |
| pending | Тот же контент: новый токен и cleanup_after +3 часа | Только точные pins, receipt и последний токен |
| ready | Точный replay с проверкой ready артефакта | Тот же токен и версия объекта; без повторного учёта |
| cleaning | Отказ stale | Отказ |
| cleaned | Отказ stale | Отказ |

Usage блокируется до document cleanup и изменения reservation. Успешный complete одной транзакцией создаёт ready JSON-артефакт и переносит bytes из reserved в storage. Незавершённые документы обрабатывает существующий очиститель; ready lineage удерживается GC, пока существует ready export. Tenant purge использует существующий allowlist backend_documents.

После cleaning/cleaned нельзя повторно использовать ключ через открытие reservation: PurgeDocument удаляет все версии ключа и может завершиться с задержкой. Переход в cleaned означает подтверждение физической очистки; SQL-тест вызывает finish только как контролируемое представление этого подтверждения. Реальное объектное хранилище этим тестом не проверяется. Для повторной попытки после очистки нужен отдельно спроектированный generation-key протокол или новый экспорт; автоматического обхода tombstone нет.

Написан `TestIntegrationSigningLineagePublicationLifecycle`: fencing, renewal, completion/replay, quota, content/pin substitution, session/download revocation, GC retention и cleanup accounting. Тест работает в откатываемой транзакции изолированной мигрированной БД и не запускает фотооперации. Отдельно ещё нужны конкурентные сценарии и физический object-store lifecycle. SignExport, квалифицированный signer, admission/dispatch/adoption остаются незавершёнными.
