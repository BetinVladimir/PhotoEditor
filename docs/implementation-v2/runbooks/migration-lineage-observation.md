# Read-only наблюдение lineage перед установкой

Реализация: `backend/db/lineage_observation.go` и `lineage_observation_queries.go`. Это механизм наблюдения и точного сопоставления, а не installer, migration bridge или разрешение на обновление.

`ObserveLineage(ctx, source, DefaultLineageObservationLimits())` открывает read-only repeatable-read транзакцию. Она использует только фиксированные запросы к каталогу и migration ledger; пользовательские строки и пароли не читаются. Результат содержит symbolic identities и SHA объектов, а не тексты функций или raw OID. Runtime limits:30 секунд на наблюдение,10 секунд на запрос,1 секунда lock timeout,100000 строк и64MiB выходных данных. Превышение4MiB сериализованным metadata object приводит к отказу; серверная память построения выражения этим пределом не гарантируется.

Покрываются24 семейства metadata: функции, агрегаты, extension functions, relations и TOAST options, columns/defaults, constraints, indexes, triggers, policies/RLS, views/rules, inheritance, sequences, types/enums/ranges, schema/default ACL, roles/memberships/settings, extensions и комментарии. Неизвестные поддержкой семейства не отбрасываются молча: предусмотренные unsupported-object проверки возвращают отказ. Важное различие raw ACL NULL/empty сохраняется; пустой ACL не передаётся напрямую в `aclexplode`.

## Два отпечатка ledger

- `SemanticSHA256` учитывает supported catalog и последовательность version/applied migration history. Физические row IDs и timestamps не включены в этот отпечаток.
- `ExactLedgerSHA256` отдельно фиксирует IDs, версии, applied и timestamps. Он нужен для exact witnesses, но не превращает физическую копию в portable lineage.

`Complete` означает завершённость именно объявленных projection families. `ExternalDependencyValidationRequired=true` остаётся обязательным: nonphoto/nonextension dependencies, extension binaries, seed data, software runtime и общая совместимость installer этим механизмом не проверены.

## Сопоставление

`ClassifyLineageObservation(observation, independentReferences)` принимает только полный результат с правильной versioned coverage, валидными объектами, counts и пересчитанным semantic hash. References должны быть проверены отдельно. Отсутствующий reference, неизвестный hash, duplicate/ambiguous identities или частичное наблюдение отклоняются. Полученное имя reference не является разрешением установить миграцию.

Нельзя автоматически создавать allowlist из только что полученного наблюдения и называть это подтверждением lineage. Тестовый reference в integration-тестах проверяет wiring comparator, а не доверенную production policy. Нельзя принимать один `max(version_id)` как доказательство всей схемы или разрешать Goose no-op при неизвестном каталоге.

## Фактические проверки

На изолированном восстановлении2638 выполнены18258 объектов/24 семейства и2637 ledger rows. Постоянный integration-тест подтверждает режимы транзакций на самом сервере и стабильность двух наблюдений. Всего observer tests:10 roots/26 cases PASS,0SKIP.

Отдельная контролируемая смена стоимости функции сохранила ledger, но изменила semantic hash и привела к отказу старого reference. После восстановления hash совпал с baseline. Реальный `MaxRows=1` привёл к отказу; частичный результат не классифицируется. Эти проверки не являются fresh/legacy migration acceptance.

Evidence: `work/lineage-observer-live-r3`, `work/lineage-observer-live-tests-r1`, `work/lineage-observer-drift-live-r1`. Первые SQL-отказы сохранены вlive-r1/r2; обнаруженные ACL и internal-char defects исправлены в production source.


## Operator CLI

Собранный `photo-v2-qualifications` поддерживает:

```sh
photo-v2-qualifications observe-lineage --output /absolute/observation.json
photo-v2-qualifications observe-lineage --output /absolute/observation.json --references /absolute/references.json --references-sha256 SHA256
```

`DATABASE_URL` задаётся через защищённое окружение; credentials не передаются аргументами. Пул ограничен одним read-only соединением; connect timeout5s, общий timeout40s. Output должен отсутствовать: существующие файлы и symlinks не перезаписываются. Новый canonical JSON создаётся эксклюзивно с правами0600.

References и SHA обязательны вместе. Manifest — canonical JSON до1MiB: `schema_version:1`, текущий `coverage`, `snapshots:[{identity,semantic_sha256}]`; identities/hashes должны быть уникальны. SHA/schema проверяются до подключения. Независимое утверждение manifest остаётся обязанностью оператора; команда не создаёт allowlist из собственного capture.

Без references результат имеет статус `CAPTURE_ONLY_NOT_CLASSIFIED`. Неизвестный semantic hash с references означает nonzero exit и отсутствие output. Краткий stdout всегда содержит `installation_authorized:false`, включая успешное совпадение. Это не команда установки миграций.

Фактическая CLI-проверка: `work/lineage-observe-cli-live-r1/result.json`,7.115s, source unchanged. На отдельной rehearsalDB проверены matching prior test reference, mismatched reference/no output, отказ перезаписи без DATABASE_URL и capture без references; canonical output0600. Reference взят из предшествующего тестового snapshot и не является одобренной installer policy. CLI unit suite:16 roots/33 cases PASS,0SKIP (`work/qualification-observe-lineage-cli-unit-20261021-r1/result.json`).


## Coverage v2: переносимость внутренних FK triggers

Актуальная coverage заканчивается `semantic-goose-ledger-v2`. References v1 должны отвергаться; их нельзя автоматически переписывать новым hash. После реального независимого restore обнаружено, что v1 включала OID-производные RI_ConstraintTrigger имена в identity/definition. Это давало5440diffentries для2720переименованныхinternalFKtriggers при неизменных ledger и остальных families.

V2 использует JSON-array identity: relation + FK constraint relation/name + function signature + trigger type. В SQL definition заменяется только проверенный начальный автоматически созданный trigger-name prefix; enabled mode и parent links сохраняются символически. Authored trigger names сохраняются. Неизвестный internal non-FK trigger, неожиданный prefix или неразрешённый parent приводят к отказу, не пропускаются. Raw materialized serialization/4MiB bound и external-validation boundary сохранены.

Actual independent source: восстановление принятогоdump SHA2fa59e19… с owners/ACL в отдельнуюDB (`work/native2637-reference-restore-r1/result.json`). На финальном коде (`work/native2637-catalog-compare-r3/result.json`) main/reference:18262objects/24families, semanticSHA16bf6c65346ed90f01e498f357c3b4acdc3b5397bb1895c5d45f97204a75a616, exactledger equal,0diff. Постоянный restore-equivalence integration включён в `lineage_catalog_live_integration_test.go`; observer/CLI actualPG suite17roots/41incl PASS,0SKIP (`work/lineage-restore-equivalence-tests-r2/result.json`). R1 source-control failure rawCTE prefix сохранён; исправлено построение query, ограничение не ослаблено.

Actual drift probes (`work/lineage-trigger-drift-r1/result.json`): DISABLE/ENABLE ALWAYS одногоinternalFK меняет ровно1объект из3004triggers; послеrollback исходная projection восстановлена. Неизменность restore таким образом не достигается игнорированием FK triggers.

Reference/main находятся в одном кластере: global roles/memberships общие и требуют независимой проверки по принятомуglobals witness + revieweddeployment deltas. Совпадение этих объектов в двухDB само по себе не доказывает их происхождение. External binaries/dependencies и installer authority также остаются отдельными gates; installation_authorized=false.
