# V27: запуск серии из редактора

## Реализованная цепочка

Редактор после успешного сохранения результата prompt/manual mask сохраняет отдельный immutable документный ref: `prompt_mask_ref` или `mask_target_ref`. Пиксельный `MaskRef.artifact` не подставляется вместо документа. Панель доступна при текущем выделении, документном ref, draft и целевой коллекции. Конфликт редактора, занятость и явно недоступная `semantic.transfer` блокируют отправку.

`SemanticCreate` создаёт отдельный scope по API instance, tenant, asset, version, ID/SHA маски, ID/revision коллекции. Изменение этих значений размонтирует предыдущую попытку и отменяет её AbortController. Изменение языка scope не сбрасывает.

`SemanticCreateScope.submit`:

1. Проверяет интенсивность0..1, EV−1..1 для local_exposure и длину описания≤1024 Unicode code points.
2. Сохраняет собственные копии коллекции, маски, параметров и два UUID ключа: snapshot/proposal. Пока результат неизвестен, параметры заблокированы.
3. Читает `OwnerAPI.v2Binding`; несовпадающая asset version отклоняется до snapshot. Источник не выбирается по текущему URL после await.
4. Вызывает `OwnerAPI.selectionSnapshot` с исходной строковой revision, исходным ключом и signal. Revision не преобразуется в JS Number. Ответ должен содержать1..5000 кадров, SHA digest и ещё действующее expires_at.
5. Строго декодирует SemanticEditIntent через SDK: server source binding, документный ref выделения, snapshot ID, kind/strength/EV/prompt и обязательный `preserve_persons=true`.
6. `OwnerAPI.proposeSemanticIntent` вызывает SDK с сохранёнными body/key/signal. Подтверждённый ticket открывает существующий `/runs/[id]` по `run_id`. Прямое открытие review по workflow UUID запрещено: SQL00535/00544 создаёт независимый `semantic_intents.id`, а00547 разрешает review именно по нему. Resolver parent run/workflow→semantic intent пока не реализован; до его появления переход из запуска в review остаётся незавершённым.

Панель предлагает remove_object/local_exposure/background_cleanup/retouch_region. Выбор операции не означает доступность модели: серверные qualification/capability/person/protection gates сохраняются. Люди остаются защищёнными; применение требует существующей per-target review цепочки.

## Повтор и отказ

Неизвестный транспортный результат,503 или другая нетерминальная ошибка сохраняют исходные ключи. Если snapshot уже получен, повтор не замораживает коллекцию заново. Если snapshot response потерян, используется исходный snapshot key. Повтор proposal сохраняет исходное тело. Ошибки400/401/403/404/409/410/422 блокируют дальнейший запуск до обновления editor scope; новая версия источника не подменяется молча. Abort не приводит к поздней навигации.

Функции OwnerAPI расширены совместимо: `selectionSnapshot(collection,key?,signal?)`; существующие вызовы без ключа сохраняют прежнее поведение. `proposeSemanticIntent(input,key,signal?)` использует строгий SDK decoder/transport.

## Evidence и незакрытый охват

`semantic-create.test.tsx` проверяет unavailable/unknown replay, неизвестный snapshot, source-version mismatch, отмену при смене коллекции и RU/BG locale switch без изменения сохранённого запроса. `semantic-create-wire.test.ts` проверяет реальный OwnerAPI→SDK transport, оба ключа, body, signal и строковую revision. Полный текущий photographer Vitest и typecheck записаны в [evidence](audit/v27-client-create-current.json). Инвентарь компонентов/состояний обновлён.

Эти тесты не доказывают реальный browser/native путь mask→snapshot→production API→worker→review. Не реализованы в этой панели восстановление неизвестной попытки после полного перезапуска приложения и автоматическая подготовка отсутствующих producers. Полная приёмка V27, включая реальные фото/ML, lifecycle/security и RTX3060 workload, остаётся открытой.

## Наблюдение существующего semantic intent

`SemanticParentProgress` встроен в существующий target screen. По известному semantic intent ID он читает `semanticReviewStatus` и сохраняет возвращённую сервером пару run/workflow. Intent ID не сравнивается с workflow ID. Повторные ответы не могут менять эту пару или уменьшать bigint revision. В queued/running/cancelling следующий последовательный запрос назначается через3с; waiting_review единожды вызывает обновление targets и останавливает опрос. Терминальные состояния также прекращают опрос.

Переход AppState в фон отменяет запрос/таймер; возврат в active возобновляет только незавершённое наблюдение. Ошибка требует явного повтора чтения статуса, без повторного admission. При401/403/404 `onAccessLost` очищает targets/cursor/накопленные страницы, отменяет незавершённое чтение targets и показывает отказ доступа. Следует отдельно подтвердить этот путь реальным браузером/native.

Evidence текущей панели/status chain: [v27-parent-progress-current.json](audit/v27-parent-progress-current.json). Историческое evidence предыдущей панели не доказывает правильный review ID: ошибочное предположение о равенстве workflow/intent исправлено проверкой SQL; непосредственный resolver остаётся обязательным следующим изменением.
