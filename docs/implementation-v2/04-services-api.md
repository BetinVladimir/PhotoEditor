# Сервисы и HTTP

Нормативный контракт маршрутов и тел: [OpenAPI 3.1](contracts/openapi.json), [JSON Schema](contracts/schemas.json), [каталог handlers](contracts/endpoints.json).

Логика каждого handler, транзакции, auth, ошибки, pagination, device challenge и клиентские цепочки описаны в [13-api-behavior.md](13-api-behavior.md). Общие AdmitV2/CompileV2/AdoptV2/FinalizeV2 и статусная модель — в [09-workflow-transactions.md](09-workflow-transactions.md). Согласия и ревокация — в [11-consent-lifecycle.md](11-consent-lifecycle.md).

Go services размещаются в существующих backend/internal/sessionbrain, learning, capture, semanticedit, delivery, interchange, provenance; общий orchestration — backend/internal/workflow. HTTP handlers тонкие: decode strict body, authenticate, вызвать service, преобразовать domain error. Service не пишет Rabbit напрямую, использует transactional outbox. DTO генерируются из схем; Go semantic validators повторяют ограничения, не выводимые из JSON Schema.

Новые функции не меняют исходный Apply v4, WorkingImage/Canvas/TransformChain и прежние quality safeguards. Расширение registry проходит regression qualification старых операций. Browser использует существующий access Bearer и CSRF для mutations; refresh cookie не заменяет access token.
