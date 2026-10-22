# Принятый план ускорения тестирования

> Текущий принятый план первого выпуска: [закрытый web-MVP от 2026-10-20](mvp-web-release-plan-2026-10-20.md). Для приоритетов выпуска и отложенной матрицы он имеет преимущество; полная цель V20–V32 и критические инварианты сохраняются.


План принят пользователем 2026-10-18. Полный объём V20–V32 и критерии приёмки сохраняются.

1. После изменения запускать быстрые проверки изменённого модуля и его зависимостей. После объединения пакета исправлений выполнять полный регресс.
2. Группировать падения по доказанной первопричине. Сначала воспроизводящий сценарий и исправление, затем затронутая группа. Не менять ожидания тестов для сокрытия дефектов.
3. Параллельно проверять независимые интерфейсы, CPU-обработку и контракты. Общие миграции/БД и общий Go cache использовать последовательно, с подтверждённым завершением и очисткой предыдущего запуска.
4. Перед интеграционным прогоном отдельно собирать Go. Успешная компиляция не означает прохождение тестов или приёмку функций.
5. Повторно использовать проверенные неизменяемые зависимости и подготовленные данные. Не переиспользовать загрязнённые tenant/session/job состояния и не подменять проверку актуальной сборки историческим PASS.
6. Сначала пройти загрузку, обработку, просмотр, принятие и экспорт. Затем ошибки, восстановление, права и межтенантную изоляцию. Это порядок исполнения, все проверки остаются обязательными.
7. Тяжёлые проверки качества выполнять пакетами на текущем Mac/Metal без искусственного лимита 10 ГБ. RTX installation/qualification отложены до предоставления сервера и отражаются как неподтверждённая часть приёмки.

## Текущие действия

Повторно подтверждено пользователем 2026-10-18: этот план остаётся основным.

- R7 завершён: 483 PASS / 115 FAIL; независимая очистка среды подтверждена. Исторический результат не подтверждает текущую сборку.
- Текущий immutable пакет `work/r15-canonical-02609-stable-composition-r1` содержит 236 файлов и 41 целый модуль. Фактическая root-проверенная collection: 666 уникальных сценариев (все 647 прежних +19 нового alias-upgrade модуля), SHA `982e68f67b0d5a97c0a3c68abc331af2fbbadbb928995339c1f5baa8990ea2d0`. Полный регресс 666 ещё не выполнен.
- Предыдущий прогон 182 завершён 88 PASS /94 FAIL /0 SKIP; независимая очистка PASS. Root проверил 92 failure traces ошибки SQL alias child, один catalog stats mismatch и один invalid withdrawn-policy fixture. Для них подготовлены и source-reviewed forward-only02609 и test/catalog successors; фактическое применение ещё проверяется.
- Восьмимодульный прогон201 завершён201 PASS /0 FAIL /0 SKIP; независимая очистка PASS. R2 исправляет только10 устаревших physical selected pins при неизменных236 production payloads и runtime guards; root25 controls PASS. Новая actualcollection666/41 подтверждена по текущим source pins.
- NOW full SQL40/661: session30509, `work/r15-canonical-02609-stable-composition-r2/run-/20261018-233357`. Общая Go/SQL среда занята до terminal и независимого аудита. Затем свежий physical owner/stage, реальный час expiry, wholephysical5/cleanup и exact666 union. Ни один историческийPASS/ожидание/collect не заменяет этапы.
- Whole Photographer повторён на неизменённом tracked коде: 524 files,3462 PASS/7 SKIP/0 FAIL, typecheck/export PASS; root1227 source pins и process groups сверены. Предыдущий Training failure сохранён как неразрешённая нестабильность; candidate helper не принят. Browser41/семьskipped gates отдельно обязательны.
- Отдельно продолжается завершение пути авторской геометрии и loupe. Локальные проверки компонентов не заменяют проверку полного пути сохранения, обработки, принятия и публикации.
- После целевой диагностики: независимая очистка, проверка сборки нового пакета, исправление выявленных причин, затем полный регресс. Приёмка браузерных маршрутов, физического хранилища и качества реальных фотографий остаётся обязательной.

## Диагностика текущего полного прогона и окружений

- SQL40/661 run233357 активен. Сценарий reference-target CUDA прошёл, MPS-вариант упал на точном assert backend==cuda в helper; проверяется, является ли причиной ошибочное fixture expectation или report metadata. До доказательства helper не ослаблять. Marker60s timeout остаётся необъяснённым; retained output не подтверждает начало Go test.
- Для будущего immutable запуска source-reviewed dual-build preflight: default+integration, единый1800s deadline, оба с exec=true, отдельные process/evidence. Root14 mock controls PASS; actual Go ещё не запускался, timeout cause не доказана. Receipt `work/compile-preflight-dual-variant-root-review-r1/review.json`. Текущий run не изменяется.
- Quality unit-only run234414 завершён до исполнения тестов:2 collection errors из-за отсутствия psycopg в local-editor venv. Все18571 protected pins неизменны, process group33492 независимо отсутствует. Исправляется распределение6 whole modules между полными native/platform environments в новом immutable runner; весь scope/общий300s limit сохраняются. Нет model/photoquality PASS. Receipt `work/legacy-photo-quality-current-root-review-r1/failed-unit-result.json`.

### Подтверждённые новые разрывы и повтор quality units

- Exact CUDA expectation в PersonReport/FaceBoxes helper несовместимо с MPS fixture producer; model-lock и production backend equality сохраняются. Отдельный source successor готовится.
- depth.predict проверяет command.inputs напрямую, хотя V2 indexed transport объявляет входы через TaskArtifacts/V2InputIndex. Требуется использовать защищённый declared-input resolver с сохранением отказа для действительно неразрешённого входа. Актуальная MPS trace seq38 показывает DEPTH_INPUT_UNDECLARED; runtime-исправление ещё не принято.
- Quality R2 source/CAS review и14 offline controls PASS; actual six-module unit-only повтор запущен session6057, run234909, две последовательные группы legacy3/platform3, общий300s. Статус определяется terminal/JUnit/exactcollection/source/owned-groups, а не фактом старта. Models/photos не запрошены.

### Проверенные кандидаты исправлений

- JPEG/XMP physical fixture: root whole metadata module29PASS/0FAIL/0SKIP, реальный APP1/XMP, decodedpixels/ICC сохранены; группа независимо отсутствует. `work/jpeg-xmp-physical-root-review-r1/review.json`. Пока изолированный sourceview, host не изменён; whole6 repeat с этим payload ещё нужен.
- Backend helper: root15 новых controls +14 whole existing helper tests PASS; неверный backend/model/source по-прежнему отвергаются, actual returnedbackend не используется как expected. Exact pins/currentCAS подтверждены: `work/target-wire-qualified-backend-root-review-r1/review.json`. Это source/CPU acceptance для future immutable integration, actual native MPS scenario пока не повторён.

## 2026-10-19 — реальные preview encoder/client contracts: 4 PASS без SKIP

- Root-reviewed whole два существующих Python owner-модуля: 2 PASS /0 FAIL /0 SKIP. Они исполнили четыре существующих Vitest-сценария Region/Render с фактическими JPEG/WebP encoder bytes: 4 PASS /0 FAIL /0 SKIP.
- Изолированный runner R4: `work/preview-client-live-owner-r4/runs/4b8a8b06bd424c269a73cabeffef2b25`. Root receipt: `work/preview-client-live-owner-root-review-r4/review.json`. Все4663 source/runtime pins совпали до/после и повторно сверены с текущими файлами; owned process group независимо отсутствует.
- Сохраняются raw encoded cases, полный Vitest JSON, JUnit, stdout/stderr. R1–R3 failures сохранены: несовместимый Path.open(opener), отсутствующий WorkingImage fixture в private view, затем root construction NameError; это ошибки runner, не исправления продукта. R4 добавляет точную копию существующего fixture и корректный вызов записи, без изменения assertions/transport/producer.
- Область доказательства: синтетические пиксели, реальные encoder/ICC и существующие TS consumers, контролируемый catalog/signing transport. Это не browser/SQL/S3/model/photo-quality acceptance. Четыре прежних skipped checks подтверждены отдельным producer-fed прогоном; исходный whole Photographer отчёт3462PASS/7SKIP не переписан. Три semantic prerequisite checks остаются открытыми.
- SQL40/661 session30509 по-прежнему активен; последнее наблюдение sequence237, v27 mask.person failure-recovery PASS. Общая среда не освобождена, terminal/cleanup пока нет.
- Closed admission diagnostic R2 source reviewed:5 controlsPASS, separate savepoints/единый4s deadline/исходный отказ сохранён. `work/defect-admission-closed-diagnostic-root-review-r2/review.json`; Go/SQL исполнение ещё не выполнено.


## 2026-10-19 — diagnostic successor: fresh exact666 collection подтверждена

`work/r15-native-diagnostic-successor-r1`, index SHA aabb0f18ad598005512ff22c3f07886c1726ce2810658239a3c254766f1595e6: parent236 +11 exact reviewed payload applications =244 unique targets. Root сверил395 frozen files и повторил24 controlsPASS. Actual freshcollect `collect-/20261019-002731` terminal0: exact666 unique IDs/41 wholemodules, Counter полностью совпадает с прежним fullscope; SQL661/physical5. Sourceunchanged, eventerrors отсутствуют, processgroup независимо отсутствует. Collection SHA7e8548b821a17fbeca8b7f3cdab6bacc4213bde9109217b3b8cba3b0b503b9fd. Root receipt `work/r15-native-diagnostic-successor-root-review-r1/review.json`.

В successor включены reviewed backendhelper, depth/scene declaredinput fix, JPEG/XMP fixture, legacy protectionfixture, settlement observer и closed diagnostic; dual default+integration compilepreflight единый1800s. Authoredcompose WIP не включён. SQL/physical execution ещё не начат: parent session30509 остаётся live, требуется terminal и независимая очистка. Fresh collection не является PASS функциональности. Обязательный стартовый disk guard PHOTO_PACKET_MIN_FREE_BYTES=17179869184 сохранён; первый вызов без него отказал до создания запуска.

