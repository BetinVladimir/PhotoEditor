# Биллинг PhotoEditor

Спецификация от 21 октября 2026 года для дальнейшей кодогенерации. Разделы 01–10 описывают целевой контракт. Фактически реализованные этапы, миграции и проверки перечислены в 11–17; их наличие не означает включение всех коммерческих функций. Stripe остаётся test-only.

Цель — оплачиваемые планы, докупка фото и отдельных операций, самостоятельная подписка на диск, учёт трафика, дешёвая оптимистичная блокировка новых запросов, онбординг с юридическими документами и удаление профиля с очисткой PII. Нужны подробные контракты на понятном языке: таблицы и функции описаны без исполняемого кода.

## Для исполнителя (читать первыми)

**Начать с [START.md](START.md).**

| Файл | Зачем |
|---|---|
| [RULES.md](RULES.md) | Нумерованные запреты и инварианты, чек-лист перед коммитом |
| [TASKS.md](TASKS.md) | Атомарные задачи и шаблон для новых |
| [EXAMPLES.md](EXAMPLES.md) | Образцы SQL-функции, записи в functions.json, миграции |
| [OPEN.md](OPEN.md) | Открытые параметры: что нельзя реализовывать |
| [TESTING.md](TESTING.md) | Тесты вместе с кодом, шаблон описания e2e, протокол приёмки модуля |
| [STATUS.md](STATUS.md) | Журнал прогонов и состояние этапов |
| [QWEN-PROMPT.md](QWEN-PROMPT.md) | Что грузить в контекст малой модели, системный промпт, порядок проверки |
| [briefs/](briefs/README.md) | Короткие карточки этапов, сгенерированные из contracts/ |
| [CODE-REVIEW.md](CODE-REVIEW.md) | Ревью кода: что проверено, находки, что не проверено |
| [ERROR-CODES.md](ERROR-CODES.md) | Коды ошибок SQL и их обработка в Go |
| [backlog/](backlog/README.md) | Сгенерированные задачи: таблицы, функции, API, Stripe, Paper UI, e2e E01–E90 |
| [STRIPE-CONFIG.md](STRIPE-CONFIG.md) | Конфигурация Stripe без ключей и порядок добавления ключей позже |
| [EXAMPLES-GO.md](EXAMPLES-GO.md), [EXAMPLES-API.md](EXAMPLES-API.md), [EXAMPLES-TESTS.md](EXAMPLES-TESTS.md) | Образцы из живого кода |
| tools/check-static.sh | Все статические проверки одной командой |
| tools/build-pack.py | Собирает пакет контекста под окно 32K и проверяет бюджет |
| tools/lint-docs.py | Проверка пробелов, ссылок и символов (`--fix` чинит пробелы; проверяет и размеры секций) |

## Навигация

| Документ | Содержание |
|---|---|
| [01-current-state.md](01-current-state.md) | Источники, проверенное состояние, расхождения и точки интеграции |
| [02-product-and-quotas.md](02-product-and-quotas.md) | Тарифы, операции, жизненный цикл, оптимистичные проверки |
| [03-data-model.md](03-data-model.md) | Поля, связи, ограничения, индексы и защита данных |
| [04-functions-and-api.md](04-functions-and-api.md) | Хранимые функции, транзакции, сервисы и API |
| [05-stripe-test-setup.md](05-stripe-test-setup.md) | Настройка тестового Stripe, платежи, восстановление |
| [06-paper-ui.md](06-paper-ui.md) | Только React Native Paper в apps/photographer |
| [07-legal-and-erasure.md](07-legal-and-erasure.md) | Онбординг, cookies, GDPR, удаление PII и юридические пробелы |
| [08-delivery-migrations-e2e.md](08-delivery-migrations-e2e.md) | Декомпозиция, миграции, положительные и отрицательные e2e, готовность |
| [09-codegen-contract.md](09-codegen-contract.md) | Уточнённые правила, DTO, FK, атомарность и очередь кодогенерации |
| [10-audit-and-traceability.md](10-audit-and-traceability.md) | Найденные расхождения, источники, исправления и дополнительные тесты |

## Приоритет решений

0. После сверки 21.10.2026 неоднозначности устранены в самих разделах 01–08; раздел 09 фиксирует точные дополнительные контракты. Если пример менее подробен, применять явный контракт 09, а не угадывать поля. Блокеры публикации не блокируют генерацию закрытого тестового каркаса.
1. Последние требования владельца: оптимистичная коммерческая блокировка и весь UI только в `apps/photographer` на React Native Paper.
2. Этот комплект уточняет прежний billing prelaunch plan от 18.10.2026. Тарифные числа сохранены. Требование прежнего плана атомарно резервировать каждую коммерческую единицу заменено ограниченными порциями локального допуска. Строгие ограничения безопасности, физического хранения и ресурсов исполнения сохраняются.
3. Реальные SQL/Go/клиентские контракты определяют существующие ограничения. Новые функции должны встраиваться в них, а не заменять authorization, consent epoch, purge, idempotency или task adoption.
4. Остальные docs остаются источником контекста, но их наличие не подтверждает реализацию или успешное выполнение тестов.

В этом комплекте технический default — выбранное исходное значение для закрытой реализации, которое меняется только новой версией конфигурации; это не найденный в исходниках факт. Открытые цены тяжёлых операций, max-file и юридические сроки нельзя выдумывать. Для закрытого тестового контура разрешены явно обозначенные fixtures; продажи недоопределённого продукта закрыты.

## Границы готовности

Тестовый биллинг готов после выполнения всех обязательных сценариев раздела 08 в изолированном Stripe Sandbox и реальной связке UI → API → PostgreSQL → очередь/worker → object storage. Live-продажи имеют отдельные блокеры: юридические дополнения, налоги, сроки retention, benchmark тяжёлых операций и capacity gate Studio Pro. Наличие тестовой оплаты не снимает эти блокеры.

Весь интерфейс продукта создаётся только в `/Users/freelancer/Documents/PhotoEditor/apps/photographer` средствами React Native Paper. `apps/portal` не получает форм управления биллингом. Hosted Checkout и Customer Portal Stripe являются внешними страницами провайдера, открываемыми из Paper UI.

## Реализация

Фактически реализованное покрытие, команды проверок и следующий этап: [11-implementation-progress.md](11-implementation-progress.md). Начальные DB-контракты находятся в `contracts/`; каркас каталога закрыт для runtime и продаж.

Учёт квот и журнал, фактически реализованные в этапе 1Б.1: [12-quota-kernel.md](12-quota-kernel.md). Слой 1Б.1 сохранён как закрытый accounting kernel; его подключение к стандартному Apply описано в этапе 1Б.2.

Рабочее подключение стандартного Apply v3/v4 (admission, canonical success, failure/cancel, reruns и restart recovery): [13-standard-apply-workflow.md](13-standard-apply-workflow.md). По умолчанию выключено, требует явного операторского opt-in для этого workflow; остальные workflow boundaries и продажи не считаются покрытыми.

Подключение SessionAnalysis.Apply и SaveEdits со стилем, единственный debit дочернего Apply и manual-free сохранение: [14-routed-apply-workflow.md](14-routed-apply-workflow.md). Миграция 02604 расширяет существующий opt-in на эти маршруты.

Квоты standalone/batch V2 render и отложенный durable settlement: [15-v2-render-workflow.md](15-v2-render-workflow.md). Отдельный opt-in выключен по умолчанию.

- [16-editor-effects-workflow.md](16-editor-effects-workflow.md) — единый Editor ticket, дочерний final render, bounded settlement и тесты.

Квоты отдельных фото-операций: [17-standalone-operations.md](17-standalone-operations.md). Миграция 02607, отдельный default-disabled operation cohort, context policies и canonical proof settlement. Цены/benchmark не выдумываются; специализированные success contracts требуют своих адаптеров.

Selected/final adapters и границы незавершённой runtime-приёмки: [18-selected-final-operations.md](18-selected-final-operations.md).
