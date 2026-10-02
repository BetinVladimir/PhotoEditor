
### Итоговая live-проверка runtime P05

Единый прогон на PostgreSQL, RabbitMQ и versioned MinIO проверяет claim identity, single canonical result, attempt/fence replay conflict, cancellation between claim and commit, expired lease, hard deadline, worker revocation, reaper retry, parallel claim, heartbeat/redelivery, ACK only after durable commit, poison dead-letter, sanitized DLQ replay, exact gateway input scope, artifact expiry и immutable object versions. 24 integration tests и Go suites `internal/queue`, `internal/workflow`, `internal/gateway`, `internal/objectstore` прошли. Это заменяет ранние пометки о недоступном локальном Docker и закрывает P05; аппаратная квалификация конкретных GPU/model locks остаётся P01/P18.
