# Установка depth/person подготовки V28

Статус: код написан; tests/build/migrations NOT RUN. Конфигурация не импортирует qualification и не включает operation registry автоматически.

Backend читает локальный JSON по `PHOTO_V28_RELIGHT_ANALYSIS_CONFIG`. Отсутствующая переменная оставляет endpoint `/api/v1/tenants/{t}/projects/{id}/relight-analyses` неустановленным (503). Подключение использует `V28_RELIGHT_SCHEDULER_DATABASE_URL` и явно выбирает `photo_scheduler`. Pool закрывается при завершении backend. При включённой конфигурации startup требует SQL admission/receipt/array locator/registration functions до установки сервиса.

Манифест: объект с `schema_version:1` и `projects` (1..2048 записей). Каждая запись содержит:

- `tenant_id`, `project_id`, `operation` (`depth.predict` или `mask.person`). Пара tenant/project плюс operation уникальна.
- `runtime`: `backend`, `model_lock_sha`, `runtime_lock_sha`, `device_profile_sha`, `qualification:{artifact_id,sha256}`, `resource_estimate:{pixel_count,ram_bytes,scratch_bytes,output_bytes,vram_bytes}`.
- `dataset_sha`, `issuer_id`, `required_checks`, `test_fixture_shas`, `evidence_shas`, `calibration_shas`, `license_cleared`.
- `capacity`: те же пять числовых ресурсных полей, верхние границы установленной среды.

SHA и UUID должны ссылаться на реально установленные и независимо квалифицированные объекты. Fixture/evidence sets — операторская acceptance policy; calibration/test overlap запрещён. Runtime reserve не превышает capacity. Для MPS capacity VRAM <=10,000,000,000 bytes; CPU VRAM=0. Эта граница admission не заменяет измерение аппаратного пика и настройку allocator на этапе глубокого тестирования.

Сервис загружает accepted WorkingImage по snapshot/base binding, выводит минимальный резерв по его массивам и повышает deployment reserve до этого минимума, сохраняя верхнюю границу capacity. Worker профиль включает renderer_version=v2.2 и полный input transport. Установленные depth/person runtime authority callbacks должны измерять те же model/runtime/device/backend pins. Изменение исходников runtime требует обновлённой квалификации.

Публичный запрос содержит только schema_version/snapshot_id/base/operation и обязательный Idempotency-Key. Ответ202 — JobTicket. Квитанция историческая; фактическое состояние и результаты получать через authenticated workflow/run endpoints. Повтор ключа сохраняет идентификаторы и требует неизменного плана. После неоднозначного commit orchestration читает сохранённый receipt и не создаёт новый запуск.

Пока отсутствует подключённый клиентский missing-input flow и OpenAPI-дополнение; нет доказательства live startup/SQL/worker исполнения. Полная приёмка остаётся отдельным этапом после завершения реализации.
