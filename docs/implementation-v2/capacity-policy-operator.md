# V32: установка и ротация CapacityPolicy

Статус: код и тесты написаны, команды и миграции не выполнялись. Этот документ не является evidence успешного rollout.

## Полномочия и транзакции

Команды доступны в существующем `photo-worker` CLI. Единственный источник DSN — `PHOTO_OPERATOR_DATABASE_URL`. `DATABASE_URL` worker не используется. Оператор должен иметь права установки реестра и вызова закрытой функции ротации. API/worker/scheduler не получают таких прав.

Файл читается существующим bounded/no-follow loader, проходит полную строгую валидацию; canonical UTF-8 bytes и SHA сохраняются в реестре. Установка и ротация выполняются в одной транзакции с statement timeout 30s, lock timeout 5s. Receipt выдаётся после успешного commit. При исключении вызывающий код не получает подтверждения; неопределённый исход commit требует чтения текущего pin, а не слепой подмены CAS.

## Первоначальный rollout

1. Остановить scheduler/workers по процедуре обслуживания.
2. Применить миграцию01625 штатным мигратором. Эти инструкции сами её не применяют.
3. Для каждой используемой policy выполнить `photo-worker capacity-policy-install /absolute/repository/path/configs/capacity-metal-verification.json` с операторским DSN в окружении. Сверить receipt SHA с планируемым pin.
4. Применить01626–01769 штатным мигратором. Exact-definition guards должны отказать при drift; запрещено удалять guard ради продолжения.
5. Существующие pins должны ссылаться на установленную policy. Legacy worker без pin сохраняет30s/60s. Неизвестная pinned policy блокирует startup/report/admission.
6. Запускать worker с тем же файлом `PHOTO_CAPACITY_POLICY`; startup сверяет SHA/offline/lease. Затем получить свежий capacity report и возобновить scheduler по штатной процедуре.

Metal verification policy сохраняет GPU budget10,000,000,000 bytes. Изменение timing не меняет бюджеты узла, число slots, runtime qualification или алгоритмы обработки.

## CAS-ротация

Пример формы команды (подставляются реальные worker UUID, DB principal, node и текущий SHA):

```sh
photo-worker capacity-policy-rotate /absolute/repository/path/configs/capacity-metal-verification.json WORKER_UUID DB_PRINCIPAL NODE --expected-sha CURRENT_SHA
```

Для впервые pin-уемого legacy worker используется `--expect-legacy` вместо `--expected-sha`. Ровно один вариант обязателен.

Ротация связывает worker/principal/node и ожидаемый старый pin, обновляет все enabled registrations этого worker/principal и сбрасывает observed SHA/time. Несовпадение любой регистрации отклоняет всю транзакцию. Новый worker запускается с тем же установленным файлом. Старый heartbeat отклоняется; новый report обязателен до admission. Same-pin CAS (`expected=next`) сохраняет уже свежий report.

При потере ответа commit оператор читает текущие регистрации и сопоставляет их с исходной операцией. Повтор со старым expected SHA не считается успешным по факту совпадения next: CAS conflict требует reconciliation. Не редактировать policy SHA и observed поля вручную для обхода проверок.

## Restore

Реестр policy — глобальная операторская конфигурация, восстанавливается вместе с БД. Restore recovery очищает worker liveness/observed reports и node capacity/report identity, сохраняя pins, qualification, budgets и enabled flags. Final fence требует отсутствия восстановленных grants. Свежие reports разрешаются только после завершения изолированной recovery и отдельно разрешённого возобновления.

## Незавершённая приёмка

Необходимо выполнить authored unit/SQL тесты, применить цепочку в изолированной БД, проверить drift guards, multi-connection rotation/claim/renew, lost commit reconciliation, restore drill и Metal measurements. До этого работоспособность deployment не подтверждена.


## Чтение после неопределённого commit

`photo-worker capacity-policy-reconcile POLICY_PATH WORKER_UUID DB_PRINCIPAL NODE --expected-sha ORIGINAL_SHA` (либо `--expect-legacy`) выполняет только read-only repeatable-read snapshot. Файл должен совпадать с исходной next policy.

`matches_next` означает совпадение текущего состояния с next pin, но не доказывает, какой оператор выполнил commit. `matches_expected` означает исходный pin; `conflict` — иной pin/node или неоднородность регистраций; `missing_registration` — отсутствие enabled scope. `policy_installed` проверяется отдельно: совпадение pin при отсутствующей policy не даёт допуска. Команда не запускает worker, не повторяет CAS и не меняет отчёты. Snapshot ограничен10,000 registrations; превышение возвращает ошибку вместо частичного доказательства. Между чтением и дальнейшим действием состояние может измениться, поэтому запись всегда остаётся CAS.


01632 применяет установленный `scratch_multiplier` однократно при резервировании под блокировкой узла. Профиль задачи хранит исходную оценку, reservation — ceil(base × multiplier). Увеличение multiplier может оставить ранее помещавшиеся задачи в waiting_resource; worker/report policy должны соответствовать новому pin. Миграция и новые тесты пока NOT RUN/NOT APPLIED.


## V32 scheduler service (написано, NOT RUN)

`PHOTO_V32_SCHEDULER_POLICY_SHA256` включает runtime из установленной policy. Некорректный SHA отклоняется до внешнего IO; отсутствующая policy или занятый leader lock останавливают startup. Единственная leader session обслуживает CPU/GPU/signer. Ошибка runtime прекращает дальнейший dispatch, readiness остаётся false; автоматический fallback и повтор неизвестного commit не выполняются.

При включении legacy picker исключает все `runs.kind=v2`. Старые scheduler binaries должны быть остановлены до переключения: их original picker не знает о partition. V2 runs обязаны иметь frozen config_snapshot.queue_class из interactive/capture/batch/training. Отсутствующая/невалидная class не заменяется автоматически и существующие snapshot не переписываются. Обновление всех producer paths и rollout старых запусков ещё остаётся открытым; включение не считается готовым к production до этой сверки и integration acceptance. Policy SHA не устанавливается автоматически в deployment.

Новые capture drafts получают frozen queue_class=capture до хеширования (01637). Исторические capture snapshots не изменяются; их продолжение при включённом V32 и покрытие остальных producer-путей ещё требуют отдельного rollout.

Новые lens drafts получают frozen queue_class=batch (01638); begin/store отказывают чужому классу. Historical absent-field configs не переписываются и остаются вне V32 candidate path до отдельного rollout.

Новые original composition drafts получают frozen queue_class=batch (01639), preflight/begin отказывают foreign class. Профили обработки и исторические snapshots не переписываются.

Новые render configs: batch в bulk compiler, interactive в preview coordinator (01640). Recovery проверяет класс без изменения task identity/profile.

Новые session analysis drafts фиксируют batch до config hash (01641). Training recovery пересобирает config и требует отдельной совместимой доработки; coverage producer-путей ещё неполон.

Training producer/recovery доработаны совместно (01642): новые drafts training, исторические receipts без класса пересобираются без изменения config hash. SQL связывает recovery class с frozen run config. Ранее отмеченный разрыв training recompilation закрыт кодом, приёмка NOT RUN.

Semantic transfer configs фиксируют batch (01643). Актуальное покрытие и незакрытые producer-пути: [capacity-queue-producer-coverage.md](capacity-queue-producer-coverage.md). Наличие этих изменений не означает готовность всего rollout.
