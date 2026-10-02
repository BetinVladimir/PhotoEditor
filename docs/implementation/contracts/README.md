# Машиночитаемые контракты

`tables.json` — декларативная полная модель БД для генерации goose/sqlc по правилам02. `endpoints.json` — полный route/service/type registry, DTO04/15. `operations.json` — операции и методы06–08. `work-packages.json` — порядок и acceptance13. `source-inventory.json` — фактический snapshot собственных файлов и Python symbols. Эти JSON — документация для генератора, не подмена executable SQL/OpenAPI/Pydantic.

`task-command.schema.json` и `task-result.schema.json` — строгие JSON Schema2020-12 для wire envelopes. Semantic DB matching/finite numbers/permissions обязателен сверх схемы. Весь immutable input manifest может превышать128 refs: command содержит ref на этот manifest, а не тысячу изображений. Роль каждого входа зафиксирована InputT operation.

`examples/*` — синтетические fixtures; повторяющиеся цифры SHA и UUID — тестовые значения, не реальные артефакты и не веса. Fixed style имеет quality.status=example, непригоден для обычного production apply. Полная style schema и измеренный adaptive пример генерируются P09/P10 из нормированных контрактов07/15; нельзя выдавать пример за обученный validated профиль.

Все schemas, Pydantic и generated clients при реализации проходят drift check. Реестры дополняются только вместе с нормативными документами, migration и test mapping.
