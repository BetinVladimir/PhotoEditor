# Установка квалифицированного Render

Статус: процедура подключения; полный RTX-сценарий ещё не принят. Актуальные фактические результаты находятся в `audit/rtx-photographer-journey-2026-10-21.md`.

## Разделение входов

Оператор использует четыре различных документа:

1. Измерения и подтверждающие файлы: исходники, описание устройства, runtime lock, результаты качества, ресурсы и encoded output. Диагностический `evidence.json` сам по себе не является qualification report.
2. Qualification report: канонический документ существующей схемы, с действительно выполненными checks и точными SHA подтверждающих файлов. Нельзя переименовать `MEASUREMENT_ONLY_NOT_QUALIFIED` в `passed`.
3. Deployment locks: независимо проверенный локальный allowlist operation/version/backend, model/runtime/device/dataset, issuer, checks, evidence, fixtures и calibration. Locks не выводятся автоматически из присланного report. DBA policy должна соответствовать тем же значениям.
4. Render calibration: отдельный single-profile объект, на который указывает `calibration_sha` в native installation pin. Go scheduler загружает внешний каталог `{"schema_version":1,"profiles":[...]}`; SHA каталога отличается от SHA отдельного профиля.

CPU остаётся backend `cpu`, даже если измерение выполнено на машине с RTX. Конфигурация `gpu_render` обозначает существующий логический класс задачи `gpu`; она допускает квалифицированный CPU provider. `cpu_precompose` предназначен только для backend/class `cpu` и операции `editor.layers_precompose`.

## Публикация и импорт

`photo-v2-qualifications import` принимает уже готовый versioned artifact. Подкоманда `publish` соединяет существующие `NewReportOperator` и `PublishAndImport`: резервирование, запись неизменяемого объекта, чтение точной версии и сверка байтов, завершение публикации, затем отдельный import.

Команда работает через отдельную непривилегированную LOGIN с ролью `photo_maintenance`. `GuardApplicationRole` и `RequireReportPublicationSchema` проверяют runtime authority. Не использовать суперпользователя для запуска команды. SQL independently проверяет operator allowlist, активное owner/admin membership, проект, policy и report. Публикация не создаёт policy, не включает admission и не устанавливает native renderer.

Пример после сборки CLI:

```sh
photo-v2-qualifications publish \
  --tenant TENANT_UUID \
  --project PROJECT_UUID \
  --operator OPERATOR_UUID \
  --report-file /absolute/reviewed-report.json \
  --sha256 REPORT_SHA256 \
  --locks /absolute/independently-reviewed-locks.json \
  --idempotency-key OPERATOR_GENERATED_KEY
```

Параметры PostgreSQL, S3 и доверенного TLS передаются через защищённое deployment environment; секреты не помещаются в аргументы или отчёты. При неопределённом результате записи сохранить ключ, файлы и SHA: повтор должен пройти существующий version-pinned recovery. Не удалять объекты и не возвращать квоту вручную, пока protocol не подтвердил завершение writer и физическую очистку.

## Подключение renderer

После подтверждённого import команда `configure-render` вызывает существующую maintenance-only `photo.configure_native_render_installation`:

```sh
photo-v2-qualifications configure-render \
  --tenant TENANT_UUID --project PROJECT_UUID \
  --qualification QUALIFICATION_UUID --report-artifact REPORT_ARTIFACT_UUID \
  --sha256 REPORT_SHA256 \
  --descriptor /absolute/render-installation.json \
  --descriptor-sha256 DESCRIPTOR_SHA256 \
  --enabled true
```

`--enabled` обязателен и допускает только `true` или `false`; значения по умолчанию для активации нет. Descriptor — canonical JSON до64KiB с точными полями `schema_version:1`, `tenant_id`, `project_id`, `operation`, `qualification_pin`, `icc_lock_sha`. Tenant/project должны совпасть с аргументами. `qualification_pin` содержит ровно шесть native pins. Допустимы `render.compose` и `editor.layers_precompose`; для последней backend только CPU. Команда не обращается к S3 и не создаёт qualification. Она проверяет maintenance LOGIN и считает false/NULL от SQL отказом.

Используя существующую функцию, Передавать реальные qualification UUID, report artifact UUID/SHA, полный native pin и ICC SHA; false означает отказ установки. Вставка строк в `native_render_installations` напрямую не заменяет эту границу.

Worker получает `PHOTO_NATIVE_RENDER_ENABLED`, `PHOTO_NATIVE_RENDER_CONFIG`, `PHOTO_NATIVE_RENDER_CONFIG_SHA`; независимый adopter — аналогичные `PHOTO_ADOPTER_NATIVE_RENDER_*`. Descriptor содержит profile, tenant/project, полный qualification pin, file pins runtime/calibration, ICC и byte limits. `NativeRenderInstallation.verify()` повторяет проверку runtime и SQL authority; отсутствующий verifier не считается успешным.

Scheduler получает `PHOTO_RENDER_CALIBRATION_FILE` и `PHOTO_RENDER_CALIBRATION_SHA256`. Обязательный startup gate сохраняется. Успешная загрузка каталога не доказывает качество, существование подходящего worker или полную пользовательскую обработку.

## Что требуется доказать до активации

- Соответствие всех runtime bytes, библиотек, model lock и реального устройства.
- Поддерживаемый style/layer/effect envelope и отказ вне него.
- Независимые quality checks и непересекающиеся calibration/held-out исходники.
- Resource evidence с точной семантикой: измеренный peak, наблюдаемый sample и принудительный upper bound различаются. Контейнерный bound действует только при сохранённой deployment policy.
- Полное прохождение lease, ingest, Apply, adoption, review, export и скачивания с проверкой SHA, размеров и ICC.

Измеренные нейтральные CPU-сценарии и SQL-тесты не разрешают автоматически все AI-операции. Общая цель V20–V32 остаётся открытой до приёмки полного функционального охвата.
