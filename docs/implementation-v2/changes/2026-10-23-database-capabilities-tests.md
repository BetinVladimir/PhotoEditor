# Покрытие тестами: общая миграция и capability

Дата: 2026-10-23. Статус: обязательный план проверок; PASS ставится только по реальному выполнению текущего кода.
Спецификация: [правки](2026-10-23-database-capabilities.md).

## Организация

Основной путь — штатный мигратор всей БД, затем тесты функций. Тесты не создают вторую систему миграций и не исполняют файл 02660 самостоятельно. Все временные исходники, конфигурации и evidence находятся внутри PhotoEditor/work. Отдельная БД/контейнер должен проверяться на собственное имя и отдельный system_identifier перед fixture; рабочая БД для fixture запрещена. Новых глобальных ролей тесты в рабочем кластере не создают.

Перед тяжёлым локальным запуском: свободно минимум 16 ГиБ; для больших архивных тестов действуют их отдельные требования. Кэши без отдельного разрешения не удаляются. Конкурирующие сборки/инфраструктурные изменения сериализуются существующими блокировками. Тесты API/клиента выполняются на локальной установке; RTX не требуется для реляционных capability cases.

## Матрица обязательных проверок

| ID | Уровень / сценарий | Проверяемый результат |
|---|---|---|
| DB01 | Пустая отдельная БД → штатный migrate полной цепочки | Все embedded migrations применились, код 0, установлен последний head, ValidateInstalledSecurity успешен. |
| DB02 | Отдельная БД с полной принятой 2659 → штатный migrate | Создан новый capability ABI; историческая функция и данные сохранены; журнал содержит ровно нужные новые применения. |
| DB03 | Повтор migrate на current | Код 0; schema/data/ACL и история применений не меняются. |
| DB04 | Ошибка/несовместимый preimage на отдельной БД | Ненулевой код, понятный закрытый статус; новая функция не остаётся частично установленной в транзакции своей миграции; API не заявляет ready. Более ранние успешно завершённые миграции могут оставаться применёнными. |
| DB05 | Полный каталог после чистой установки и upgrade | Сопоставить семантические сигнатуры, тела, owner/search_path/ACL, constraints/policies/indexes. Не требовать равенства межкластерных OID, timestamps или системных идентификаторов. |
| DB06 | Старые данные | Пользовательские записи, tenant связи, qualification, workers, выполненные задачи и результаты не переписаны; версии старых задач сохранены. |
| SEC01 | Новая функция: owner/SECURITY DEFINER/search_path | photo_fn_owner, фиксированный безопасный путь, ожидаемый ABI. |
| SEC02 | PUBLIC/worker/scheduler/gateway/maintenance | Нет EXECUTE на новой функции; API имеет необходимый минимальный EXECUTE. Нет новых широких table grants. |
| SEC03 | Два tenant, чужой project | API запрещает чужой tenant/project; evidence и worker одного tenant не повышают доступность другого. Проверка выполняется реальными ограниченными principals, а не только superuser. |
| CAP01 | Две registry-версии одного имени | API возвращает разные точные пары operation/version; дубликатов пары нет. |
| CAP02 | Legacy worker допускает compose/1 | compose/2 не становится доступной. |
| CAP03 | Authored worker с действующей установкой compose/2 | compose/2 доступна только на CPU; compose/1 не наследует authored evidence. |
| CAP04 | Отзыв policy/installation | Нужная версия становится недоступной; соседняя версия не получает допуск. |
| CAP05 | v2_enabled=false для конкретной версии | Эта версия недоступна, даже если worker рекламирует имя; соседняя версия проверяется независимо. |
| CAP06 | authored_segment/geometry без собственной установки | Недоступны; наличие compose installation не заменяет их установку. |
| CAP07 | region.authored_crop: enabled + имя worker без законченной установки | Недоступна с честным состоянием; тест не подменяет missing runtime фиктивным ready. |
| CAP08 | Устаревший heartbeat/node/policy или disabled worker/node | Доступность снимается; только свежий процесс не заменяет qualification. |
| CAP09 | signer/general execution pools и signer expiry | Сохраняется изоляция; истёкший signing qualification не продлевается heartbeat. |
| CAP10 | CPU/CUDA/MPS, неверный backend/model lock | Backend не появляется вне поддержанного и квалифицированного набора; клиентский OS не источник server availability. |
| API01 | Реальный endpoint после общей миграции | Все строки имеют operation_version; корректные JSON-типы/status/reason; существующие поля API сохранены. |
| API02 | API стартует на старой/неполной схеме | Старт отказывает до публикации доступности; startup не выполняет DDL. |
| API03 | Semantic installation mask | Маска только сужает доступность и не переносится на неизвестную версию. |
| UI01 | readCapabilities: две разные версии | Обе принимаются как отдельные строки; старый versionless и дубль точной пары отклоняются. |
| UI02 | operationAvailable: другая версия ready | Запрошенная версия остаётся недоступной; порядок строк не влияет на ответ. |
| UI03 | Все capability call sites | Версия совпадает с реально формируемым producer contract; legacy v1 и authored compose v2 проверены отдельно. Нет переключателя версии для пользователя. |
| UI04 | Редактор, сессии, semantic и multi-image routes | Недоступные действия блокируются; смена actor/tenant/route и stale capability не отправляют новый чужой запрос. Уже принятый запрос и восстановление uncertain submission сохраняют корректную семантику. |
| UI05 | Доступная функция → обработка | Штатный UI выбирает корректный контракт и результат; никаких обходных demo-only endpoint. Проверяется реальный путь, а не только кнопка. |
| EXP01 | Scope117 / product109 | Старые 114 записей сохранены, три добавления включены; identity count не смешан с version rows. |
| EXP02 | Версионные qualification references | Evidence прикрепляется только к той же версии; passed report другой версии не меняет API status. |
| EXP03 | Реальный owner API + read-only DB capture | Создан ограниченный отчёт, source/scope/function pins совпадают; missing qualifications отмечены как пробел, не ready. |
| EXP04 | Секреты, I/O, ошибки | FD-only token/DSN, отсутствие секретов в output/log; bounded body/rows/deadline, create-only output; нет login/refresh/retry внутри экспортёра. |
| EXP05 | Истёкший token / неизвестная строка / повтор пары / отсутствующий ABI | Отказ без синтетического отчёта. Отдельное штатное обновление сессии учитывается как auth mutation, не маскируется read-only capture. |
| EXP06 | Observer permissions | Privileged диагностический capture явно помечен; не считается доказательством ограниченной observer-роли. Отсутствующие права не заменяются неявным admin fallback. |

## Тестовые данные и ограничения

Для SQL нужны две независимые tenant/project области, существующие ограниченные worker principals, действующая политика и настоящий relational qualification setup. Существующие fixture 02650 допускается переиспользовать; нельзя заменять pair_evidence заглушкой true или отключать RLS/constraints. Проверки native model/фотографического качества не подменяются реляционными fixture.

Минимальная комбинация версий: render.compose/1 и /2; для каждой отдельно disabled/enabled, отсутствие/наличие точной установки, отзыв политики. Sibling cases: render.authored_segment/1, render.authored_geometry/1. region.authored_crop/1 остаётся отдельным негативным case до реализации полной цепочки.

## Что изменить в существующих тестах

- work/versioned-capability-authority-r1/sql-tests/test_versioned_capabilities.py сейчас сам исполняет migration SQL и ожидает head2659. До приёмки перевести fixture на уже полностью мигрированную БД: удалить DDL из fixture, проверять текущий head и наличие ABI, не выдавать direct-SQL запуск за общий migrate.
- Перенести согласованные функциональные SQL cases в постоянный тестовый каталог проекта, сохранив отдельный config/owned-cluster guard. Временный work-пакет — evidence/предложение, не единственное место продуктовых регрессий.
- Дополнить существующие восемь SQL cases тестами свежести, tenant isolation и signer-пула из матрицы; переиспользовать покрывающие текущие тесты с явной ссылкой, а не дублировать реализацию.
- Добавить регрессию на каждый реально найденный дефект: потерю версии, stale scope114, неподходящий version length20 и межверсионное прикрепление qualification.
- Не ограничиваться изменением mock fixtures operation_version=1. Для каждого producer/route проверить действительную версию, включая authored compose/2.

## Выявленный блокер SQL fixture

Ревью подготовленного пакета обнаружило, что старый setup 02650 формирует geometry policy/report только с quality checks. Текущая 02652 требует дополнительно authored geometry schema и calibration checks/evidence. До исполнения матрицы обновить начальное формирование fixture и обычный импорт qualification под текущий контракт. Нельзя ослаблять production pair_evidence, подменять функцию или UPDATE уже неизменяемого qualification report ради прохождения теста. Добавить положительный case с полноценной policy/report/calibration и отрицательные cases по отсутствующему/неверному каждому обязательному pin. Пока это не исправлено, collected SQL cases не являются исполнимым доказательством.

## Команды целевых source-тестов

Из backend:

```sh
go test ./internal/capability
```

Python из корня репозитория:

```sh
PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 .venv-platform/bin/python -B -m pytest -q -p no:cacheprovider tests/platform/test_v20_operation_evidence.py
```

Клиентские тесты запускаются существующим Vitest-конфигом apps/photographer/vitest.config.mjs и установленным package manager проекта. Нельзя подменять его дефолтным конфигом или считать import/setup failure продуктовым PASS. Точные SQL/CLI команды добавляются к evidence после адаптации fixture; неизвестные команды развёртывания не выдумываются.

## Доказательства и итог

На каждый прогон сохранить внутри work: commit/source hashes и dirty diff cutoff, набор embedded миграций, режим clean/upgrade/repeat, идентификатор отдельного кластера без секретов, exit code, итог head, JUnit/логи, semantic schema/security comparison, данные до/после и список актуальных API версий. Учёт credential/session refresh вести отдельно. Не публиковать DSN, access/refresh tokens, SQL secrets или приватные object links.

На 2026-10-23: canonical exporter40 tests PASS; подготовленный overlay capability Go PASS и 15 client suites/184 tests PASS; восемь SQL cases только collected. Это исторические ориентиры, не доказательство окончательной сборки. Общая установка 02660, functional SQL execution и реальный versioned API/export ещё не подтверждены.

Изменение принимается после DB01–DB06, SEC01–SEC03, CAP01–CAP10, API01–API03, UI01–UI05, EXP01–EXP06 либо документированного точного покрытия существующей проверкой. Пропущенный тест не считается PASS. Результаты физического archive5000/ZIP64 и остальных модулей ведутся отдельно и не заменяются этим пакетом.
