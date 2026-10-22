# 14. Каталог функций

Статус: спроектировано, не реализовано (2026-10-22). Начало: [README](README.md). Типы и константы — [13](13-data-structures.md); задачи, которые реализуют функции, — [15](15-task-cards.md); векторы и тесты — [16](16-test-logic.md), [`vectors/`](vectors/).

## 0. Как читать

Запись функции:

```
FN-<модуль>-nn  имя(аргументы) → результат        [язык: файл]
Пред: условия на вход.   Пост: что истинно после.   Ошибки: коды (порядок проверки = порядок перечисления!)
Алгоритм: нумерованные шаги (где есть эталон).   Векторы: файл → ID кейсов.
```

Правила, общие для всех функций:

- **Порядок проверок ошибок обязателен**: тесты проверяют, какой код вернулся при нескольких нарушениях.
- **Чистые функции** (модули T, Q, C, L, X, E, R-часть) не читают время, сеть, случайные числа и файловую систему; входной объект не изменяют (возвращают копию).
- **Целочисленное деление** во времени: Python `//`, TypeScript `Math.floor(a/b)` для неотрицательных значений (проверять `Number.isSafeInteger` результата), Go `int64 /`.
- Все три реализации (TS клиента, Go бэкенда, Python воркера) обязаны давать **один результат** на общих векторах (VID-T-01…03). Реализуются: `T-01…T-04`, `T-11`, `T-12` — в TS, Go и Python; `T-05…T-10` — в TS (редактор) и в Go (зеркало для эндпоинта `cut-applications`, который применяет границы на сервере; Go проходит те же векторы `commands.json`); `T-13`, `T-14` — только TS; `Q-01…Q-05` — Go (сервер) и TS (отображение оценок); остальные `Q-*` — только TS.

Условные константы `FT` (кадр таймлайна в тиках) и `TPS` берутся из DS-01 и `video.fps` документа: `FT = frameTicks(doc.video.fps)`.

---

## Модуль T — время, команды монтажа, документ

**FN-T-01 `frameTicks(fps: Rational) → int`**   [TS `video-timeline.ts`, Go `video/timeline/time.go`, Py `video/timeline.py`]
Алгоритм: 1) `v = TPS × fps.den`; 2) если `v mod fps.num ≠ 0` → ошибка `FPS_UNSUPPORTED`; 3) вернуть `v / fps.num`. Вектор: `time.json → frame_ticks` (8 пар; ключ `"num/den"`), `sample_ticks` (`48000→14700`, `44100→16000`).

**FN-T-02 `framesToTicks(fps, frame) → int`** = `frame × frameTicks(fps)`; `frame ≥ 0` иначе `FRAME_NEGATIVE`. Вектор: `time.json → frames_to_ticks`.

**FN-T-03 `ticksToFrame(fps, ticks, mode: 'floor'|'round') → int`**
`floor`: `ticks / FT`. `round`: `(ticks + FT/2) / FT` (целое деление, `FT/2` целое деление). `ticks < 0` → `TICKS_NEGATIVE`. Вектор: `time.json → ticks_to_frame` (поля `floor`, `round`).

**FN-T-04 `clipDuration(clip) → int`**
Алгоритм: 1) фото → `clip.duration`; 2) иначе `speed = clip.speed или 1/1`; 3) `((src_out − src_in) × speed.den) / speed.num`. Используется всеми командами; вектор — через команды (ниже).

**FN-T-05 `splitClip(doc, clipId, at, newId) → EditResult`**
Ошибки в порядке: `CLIP_NOT_FOUND` · `SPLIT_PHOTO_UNSUPPORTED` · `SPLIT_OUT_OF_RANGE` (не `clip.at < at < clip.at + dur`, строго) · `SPLIT_NOT_ALIGNED` (`at mod FT ≠ 0`) · `SPLIT_NOT_ALIGNED` (`((at − clip.at) × speed.num) mod speed.den ≠ 0`).
Алгоритм: 1) копия документа; 2) `off = at − clip.at`; `so = off × num / den`; 3) вторая часть `B` = глубокая копия клипа: `B.id = newId`, `B.at = at`, `B.src_in = clip.src_in + so`; 4) у исходного `clip.src_out = clip.src_in + so`; 5) у `B` удалить `transition_in`; 6) если есть `audio`: `clip.audio.fade_out = 0`, `B.audio.fade_in = 0`; 7) у эффектов `B` присвоить `id = newId + "-e" + k` для `k = 1…`; 8) вставить `B` сразу после `clip` в массив дорожки. Векторы: `commands.json → SP-01…SP-06`.

**FN-T-06 `trimEdge(doc, clipId, edge: 'in'|'out', pos, sourceDuration) → EditResult`**
Ошибки в порядке: `CLIP_NOT_FOUND` · `TRIM_NOT_ALIGNED` (`pos mod FT ≠ 0`) · далее по краю:
- `in`: `prevEnd = 0` для первого клипа дорожки, иначе `prev.at + dur(prev)`; `pos < prevEnd` → `TRIM_OVERLAP`; `pos ≥ clip.at + dur` → `TRIM_EMPTY`; `delta = pos − clip.at`; `(delta × num) mod den ≠ 0` → `TRIM_NOT_ALIGNED`; `ns = src_in + delta × num / den`; `ns < 0` → `TRIM_BEFORE_SOURCE`; результат: `src_in = ns`, `at = pos`.
- `out`: `pos ≤ clip.at` → `TRIM_EMPTY`; есть следующий и `pos > next.at` → `TRIM_OVERLAP`; `((pos − at) × num) mod den ≠ 0` → `TRIM_NOT_ALIGNED`; `no = src_in + (pos − at) × num / den`; `no > sourceDuration` → `TRIM_BEYOND_SOURCE`; результат: `src_out = no`.
Векторы: `TR-01…TR-07`.

**FN-T-07 `moveClip(doc, clipId, newAt, trackId?) → EditResult`**
Ошибки в порядке: `CLIP_NOT_FOUND` · `MOVE_NOT_ALIGNED` (`newAt < 0` или `newAt mod FT ≠ 0`) · `TRACK_NOT_FOUND` (если `trackId` задан и нет такой дорожки) · `MOVE_TRACK_KIND` (вид клипа: `'audio'` если `source.kind='audio'`, иначе `'video'`; должен совпасть с `track.kind`) · `MOVE_OVERLAP` (после удаления клипа со старого места: для любого `o` на целевой дорожке `newAt < o.at + dur(o)` **и** `o.at < newAt + dur(clip)`).
Результат: `clip.at = newAt`, клип добавлен в целевую дорожку, дорожка отсортирована по `at`. Векторы: `MV-01…MV-04`.

**FN-T-08 `deleteClip(doc, clipId, ripple: bool) → EditResult`**
Ошибка: `CLIP_NOT_FOUND`. Алгоритм: удалить клип; если `ripple` — для каждого клипа **той же дорожки** с `at > deleted.at` выполнить `at −= dur(deleted)`. Векторы: `DL-01`, `DL-02`.

**FN-T-09 `setSpeed(doc, clipId, num, den) → EditResult`**
Ошибки в порядке: `CLIP_NOT_FOUND` · `SPEED_PHOTO` · `SPEED_RANGE` (`num ≤ 0` или `den ≤ 0` или `num × 4 < den` или `num > 4 × den`) · `SPEED_TOO_SHORT` · `SPEED_OVERLAP`.
Алгоритм: 1) сократить дробь на `gcd`; 2) `raw = (src_out − src_in) × den / num`; 3) `d = floor(raw / FT) × FT` (округление вниз к сетке); 4) `d ≤ 0` → `SPEED_TOO_SHORT`; 5) есть следующий клип и `at + d > next.at` → `SPEED_OVERLAP`; 6) `src_out = src_in + d × num / den`; `speed = {num, den}` (сокращённые). Векторы: `SS-01…SS-05` (`SS-04`: `4/2` даёт `2/1`).

**FN-T-10 `applyCuts(doc, mediaId, boundaries, accepted, mode, newIds) → EditResult с applied[]`**
`mode ∈ {'split_at_start','split_at_middle','markers_only'}`; ошибка `APPLY_IDS_EXHAUSTED`, если `newIds` кончились.
Алгоритм: 1) отобрать границы с `id ∈ accepted`; отсортировать по `start_ticks` **по убыванию**; 2) для каждой границы: `ts = start_ticks` (`split_at_start`) или `(start_ticks + end_ticks) / 2` (`split_at_middle`, целое деление); `markers_only` использует `ts` как `split_at_start`; 3) для каждой дорожки с `kind='video'` взять **снимок** списка клипов на момент начала обработки границы; для каждого клипа снимка с `source.media_id = mediaId`: пропустить, если не `src_in < ts < src_out`; `tl = clip.at + (ts − src_in) × den / num`; `tl = ((tl + FT/2) / FT) × FT` (округление к ближайшей сетке); пропустить, если не `clip.at < tl < clip.at + dur`; 4) режимы `split_*`: `nid = следующий из newIds`; применить `splitClip(doc, clip.id, tl, nid)`; записать в `applied` `{boundary, clip, new: nid, at: tl}`; режим `markers_only`: добавить маркер `{id: nid, at: tl, kind: 'cut', label: ''}` в `doc.markers`, если нет маркера с тем же `at` и `kind`; в `applied` записать с `new = nid`. Повторное применение того же отчёта не меняет документ и не расходует `newIds` (`clip.at < tl` строго). Векторы: `AC-01…AC-04`; режим `markers_only` — кейсы `TC-T-AC-M1…M4` ([16](16-test-logic.md), раздел 4.1).

**FN-T-11 `canonicalize(doc) → bytes`, `documentSha256(doc) → hex`**
RFC 8785 (JCS): TS — [`packages/api-client/src/canonical.ts`](../../packages/api-client/src/canonical.ts); Go — [`backend/internal/canonical`](../../backend/internal/canonical); Py — `rfc8785` (`canonical_bytes`, `canonical_hash` из `contracts/base.py`). `sha256` — от канонических байтов UTF-8. Векторы: `canonical.json` (2 кейса: `canonical` строка и `sha256` обязаны совпасть во всех трёх языках; важные детали: `1.0 → 1`, `-0.0 → 0`, ключи в порядке кодовых точек, `1e21 → 1e+21`, не-ASCII без экранирования).

**FN-T-12 `validateDocument(doc, ctx: ValidateContext) → ValidationIssue[]`**
Правила VT-01…VT-10 ([07](07-timeline-edl-and-render-spec.md), раздел 4) в этом порядке; возвращает **все** найденные нарушения (до 100), каждое с JSON Pointer. Реализация правил:
1. VT-01: неизвестный ключ на любом уровне; нецелое число в поле-тиках; `schema_version ≠ 1`; пара fps не из `SUPPORTED_FPS`.
2. VT-02: размер `canonicalize(doc)` > `MAX_DOC_BYTES`; дорожек видео/аудио > 4; клипов > 2000; `max(at+dur)` > `MAX_DURATION_TICKS`.
3. VT-03: повтор `id` среди дорожек, клипов, эффектов, маркеров; `id` не по шаблону `^[A-Za-z0-9_-]{1,64}$`.
4. VT-04: на дорожке клипы не отсортированы по `at` либо `a.at + dur(a) > b.at` для соседей.
5. VT-05: `src_in < 0`, `src_out ≤ src_in`, `src_out > ctx.sourceDurations[id]` (допуск: 1 кадр источника не поддерживается — строго).
6. VT-06: `ctx.sourceStates[id] ≠ 'ready'` → `SOURCE_NOT_READY`; `ctx.sourceHashes[id] ≠ source.sha256` → `SOURCE_HASH_MISMATCH`.
7. VT-07: параметр эффекта вне диапазона [07](07-timeline-edl-and-render-spec.md) раздел 3.2 и [23](23-editor-playback-timeline.md) разделы 11.1–11.2; кривая: `x` не строго возрастает или точек не 2…16.
8. VT-08: `transition_in.duration > min(dur(clip), dur(prev))/2` в тиках целым делением; для первого клипа дорожки допустим только `fade_in`.
9. VT-09: `sidechain_tracks` ссылается на несуществующую или неаудио-дорожку, либо образует цикл.
10. VT-10: только при `ctx.forRender = true`: если `ctx.musicClipIds` непусто и `rights.music_declared ≠ true` → `RIGHTS_REQUIRED`.
Соглашения (проверяются векторами):
- **Порядок выдачи**: по правилам VT-01…VT-10, внутри правила — по порядку документа (дорожки, затем клипы, затем эффекты). Если найден хотя бы один `VT-01`, возвращаются **только** нарушения `VT-01` (структура ненадёжна).
- **Указатели**: VT-01 — на сам ключ (`/extra`, `/schema_version`, `/video/fps`, `/tracks/i/clips/j/at`); VT-02 — `/tracks`; VT-03 — на `id` **повторного** или неверного элемента; VT-04 — на `at` **более позднего** клипа пары; VT-05 — на `src_out` (на `src_in`, если он отрицателен); VT-06 — на `source` клипа; VT-07 — на конкретный параметр (`…/params/<имя>`); VT-08 — на `transition_in/duration` (или `/kind` для недопустимого вида на первом клипе); VT-09 — на `…/params/sidechain_tracks`; VT-10 — `/rights/music_declared`.
- Допустимый вид перехода на первом клипе дорожки: `fade_in` или `cut`. Лимит длительности перехода: для первого клипа `dur(clip)/2`, для остальных `min(dur(clip), dur(prev))/2` (целое деление).
- VT-06: сначала проверяется состояние (`SOURCE_NOT_READY`), и только если оно `ready` — хеш (`SOURCE_HASH_MISMATCH`).
Негативные документы и ожидаемые правила — набор `TC-T-VAL-00…20` (файл `vectors/validate_cases.json`) ([16](16-test-logic.md), раздел 4.2).

**FN-T-13 `snapTime(candidate, anchors, thresholdTicks) → int`**
Выбирает ближайший якорь (кадр, края клипов, маркеры, playhead) в пределах порога; при равенстве — меньший по времени; нет якоря — `candidate` с округлением к кадру (`round`). `thresholdTicks = SNAP_PX × ticksPerPixel`.

**FN-T-14 `invertCommand(before, command) → Command`**
Свойство: `cmd.invert(before).apply(cmd.apply(before).doc).doc` канонически равен `before` (байт-в-байт `canonicalize`). Проверка — property-тест ([16](16-test-logic.md), раздел 4.3).


**FN-T-15…28 — команды редактора** [TS `video-timeline-commands.ts`]. Нормативные определения (ошибки по порядку, результат, обратная команда) — [23](23-editor-playback-timeline.md), раздел 11; векторы `editor.json → commands` (83) и `check_locked` (9).

| ID | Команда | Обратная |
|---|---|---|
| FN-T-15 | `addClip(doc, track, clip, ripple)` | `deleteClip(clip.id, ripple)` |
| FN-T-16 | `duplicateClip(doc, clipId, newId)` | `deleteClip(newId, false)` |
| FN-T-17 | `setTransition(doc, clipId, transition \| null)` | прежний переход |
| FN-T-18 | `setEffect(doc, owner, effect, index?)` | прежний эффект или `removeEffect` |
| FN-T-19 | `removeEffect(doc, owner, effectId)` | `setEffect` с прежним индексом |
| FN-T-20 | `moveEffect(doc, owner, effectId, index)` | прежний индекс |
| FN-T-21 | `setClipProps(doc, clipId, patch)` | прежние полные значения ключей |
| FN-T-22 | `setTrackProps(doc, trackId, patch)` | прежние значения |
| FN-T-23 | `addTrack(doc, kind, trackId, index?, props?)` | `removeTrack` |
| FN-T-24 | `removeTrack(doc, trackId)` | `addTrack` с прежними индексом и свойствами |
| FN-T-25 | `addMarker(doc, marker)` | `removeMarker` |
| FN-T-26 | `updateMarker(doc, markerId, patch)` | прежние значения |
| FN-T-27 | `removeMarker(doc, markerId)` | `addMarker` |
| FN-T-28 | `checkLocked(doc, command) → TRACK_LOCKED \| null` | — |

---

## Модуль Q — квота и загрузка

**FN-Q-01 `partBytes(bytes) → int`**: `raw = ceil(bytes / 9000)`; `r = ceil(raw / 1 MiB) × 1 MiB`; вернуть `max(32 MiB, r)`. Ошибки: `bytes ≤ 0` → `BYTES_INVALID`; `bytes > UPLOAD_MAX_BYTES` → `VIDEO_TOO_LARGE`. Вектор: `quota.json → part_bytes` (пример: 16 ГиБ → 33 554 432 и 512 частей).
**FN-Q-02 `partsTotal(bytes) → int`** = `ceil(bytes / partBytes(bytes))`.
**FN-Q-03 `derivativeEstimate(bytes) → int`** = `min(bytes × 15 / 100, 4 GiB) + 128 MiB` (целое деление на 100 после умножения). Вектор: `derivative_estimate`.
**FN-Q-04 `renderEstimate(durationTicks, maxrateBps, audioBps) → int`** = `ceil(durationTicks × (maxrate + audio) × 105 / (8 × 100 × TPS)) + 64 MiB`. Использовать 128-битное/произвольной точности целое: Go `math/big`, TS `BigInt`, Py int. Вектор: `render_estimate` (4 кейса, самый большой — 68 379 268 864).
**FN-Q-05 `admit(used, reserved, limit, request) → bool`** = `used + reserved + request ≤ limit`. Вектор: `admit` (6 кейсов, включая граничные ±1 байт).
**FN-Q-06 `fileFingerprint(name, size, lastModified) → hex`** = `sha256(name + "\n" + size + "\n" + lastModified)` для возобновления загрузки.
**FN-Q-07 `hashFileStreaming(file, onProgress, signal) → hex`** [TS]: читает `Blob.slice` по 8 МиБ, `sha256` (`@noble/hashes`) обновляется по срезам; память не растёт с размером файла; `signal.aborted` → `ABORTED`.
**FN-Q-08 `uploadParts(session, file, store, signal)`** [TS]: параллельность `UPLOAD_PART_CONCURRENCY`; для каждой части: запросить URL пачкой, `PUT` среза, взять `ETag` из заголовка, записать в журнал-контрольную точку **после** успешного ответа; повтор части ≤ 3 раза с паузами 1 с, 2 с, 4 с; `403/410` от подписи → запросить новый URL; уже выполненные части (из контрольной точки) пропускаются.
**FN-Q-09 `jobCostSeconds(kind, durationTicks) → int`** [Go: `video/quota/scale.go`]: формулы и ошибки — [20](20-saas-scaling-operations.md), 3.4. Вектор: `scale.json` → `job_cost` (18 кейсов).
**FN-Q-10 `retryAfterSeconds(aheadCost, activeWorkers) → int`** [Go]: `w=max(1,activeWorkers)`; `raw=min(900,max(5,ceil(aheadCost/w)))`; округлить вверх до кратного 5; `aheadCost<0` → `AHEAD_INVALID`. Вектор: `retry_after` (11).
**FN-Q-11 `egressLeaseBytes(remaining, activeStreams) → int`** [Go]: правило — [20](20-saas-scaling-operations.md), 3.4. Вектор: `egress_lease` (12).
**FN-Q-13 `ingestDurationEstimateTicks(bytes) → ticks`** [Go]: `bytes ≤ 0` → `BYTES_INVALID`; `secs = min(10800, max(1, ceil(bytes / 1 000 000)))`; результат `secs × TPS`. Нужна, потому что длительность неизвестна до `probe`; результат подаётся в `FN-Q-09(ingest, …)`. Вектор: `ingest_duration_estimate` (7).
**FN-Q-12 `admitJob(state, limits, kind, cost) → {ok} | {ok:false, code, limit_code|retry_after_s}`** [Go]: порядок проверок — группа, суточный бюджет, порог перегрузки (кроме `preview`); неизвестный `kind` → `KIND_INVALID`. Вектор: `admit_job` (20). SQL-реализация — `FN-D-17`, те же кейсы (`TC-SC-SQL-01`).

## Модуль C — детекция границ

**FN-C-01 `postprocessBoundaries(single, many, luma, params, frameTicks) → Boundary[]`**   [Py `video/cuts.py`; тот же алгоритм используется клиентом только для тестов-зеркал не требуется]
Входы: `single[i]` — вероятность резкой границы **между кадрами i и i+1**; `many[i]` — вероятность плавного перехода на кадре i; `luma[i]` — средняя яркость кадра i (0…255); `params = {sensitivity, min_shot_frames, mode, include_flash}`; порог `THR = {1:0.8, 2:0.65, 3:0.5, 4:0.4, 5:0.3}[sensitivity]`.
Алгоритм:
1. `n = len(single)`.
2. **Вспышки**: индексы `i ∈ [1, n−2]`, для которых `luma[i] − luma[i−1] > 60` и `luma[i] − luma[i+1] > 60` и `|luma[i−1] − luma[i+1]| < 20`.
3. **Резкие**: максимальные серии подряд идущих `i`, где `single[i] ≥ THR`; для серии взять `k` = индекс первого максимума значения; кандидат `{kind:'cut', frame_in:k+1, frame_out:k+1, confidence:single[k]}`.
4. Если `mode='all'`:
   a. **Плавные**: серии `many[i] ≥ THR` длиной ≥ 3, `[a..b]`: удалить кандидатов `cut` с `a ≤ frame_in ≤ b+1`; добавить `{kind:'dissolve', frame_in:a, frame_out:b+1, confidence:max(many[a..b])}`.
   b. **Затемнения**: серии `luma[i] ≤ 24` длиной ≥ 2, `[a..b]`: `j=a`; пока `j>0` и `luma[j−1] > luma[j]` → `j−=1`; если `a−j ≥ 5` и `luma[j] ≥ 64` → кандидат `{fade_out, frame_in:j, frame_out:a, confidence:0.9}`. `k=b`; пока `k<n−1` и `luma[k+1] > luma[k]` → `k+=1`; если `k−b ≥ 5` и `luma[k] ≥ 64` → кандидат `{fade_in, frame_in:b, frame_out:k, confidence:0.9}`.
5. **Подавление вспышек**: удалить кандидатов `cut`, у которых `frame_in` ∈ вспышки или `frame_in − 1` ∈ вспышки.
6. Если `mode='all'` и `include_flash`: добавить `{flash, frame_in:i, frame_out:i+1, confidence:0.8}` для каждой вспышки.
7. **Сортировка** по `(frame_in, −confidence, kind)`.
8. **Минимальный план**: обходить по порядку; если список результата непуст и `frame_in − last.frame_in < min_shot_frames`: заменить `last` текущим, если `confidence` текущего **строго больше**; иначе пропустить; иначе добавить.
9. **Выход**: `id = "b" + порядковый номер с 1`; `start_ticks = frame_in × frameTicks`; `end_ticks = frame_out × frameTicks`; `confidence` — округление до 4 знаков.
Векторы: `cuts.json → CD-01…CD-15` (поле `input.params` задаёт параметры; отсутствие ключа = значения по умолчанию `sensitivity=3, min_shot=8, mode='all', include_flash=false`; `fps='25/1'`).

**FN-C-02 `extractFeatures(frames)`** [Py, модельный путь] — **не покрывается векторами**; проверяется метриками F1 на фикстурах ([16](16-test-logic.md), раздел 6).
**FN-C-03 `scoreBoundaries(pred, gt) → {tp, fp, fn}`**: два набора независимо: «резкие» (`cut`) и «плавные» (`dissolve`, `fade_in`, `fade_out`); `flash` не оценивается. Для каждой пары `(p, g)` одной группы: резкие — совпадение при `|p.frame_in − g.frame_in| ≤ 2`, стоимость = эта разность; плавные — `IoU` полуинтервалов `[frame_in, frame_out)` ≥ 0.3, стоимость = `1 − IoU`. Пары упорядочить по `(стоимость, индекс p, индекс g)`; жадно брать пары, у которых ни `p`, ни `g` ещё не использованы. `tp` = число пар; `fp` = предсказаний − `tp`; `fn` = разметки − `tp`; суммировать по группам. Вектор: `f1.json → F1-01…F1-10`.
**FN-C-04 `prf(tp, fp, fn) → {precision, recall, f1}`**: если `tp + fp + fn = 0` → `1, 1, 1`; `precision = tp/(tp+fp)` (0 при пустом знаменателе); `recall = tp/(tp+fn)`; `f1 = 2pr/(p+r)` (0 при `p+r=0`); округление до 4 знаков. Вектор: `expect` в `f1.json`.
**FN-C-05 `iou(a, b) → float`**: `inter = max(0, min(a1,b1) − max(a0,b0))`; `union = max(a1,b1) − min(a0,b0)`; `union > 0 ? inter/union : 0`.

## Модуль L — цвет

**FN-L-01 `srgbToLinear(c)`**: `c ≤ 0.04045 ? c/12.92 : ((c+0.055)/1.055)^2.4`. **`linearToSrgb(c)`**: сначала зажать в [0;1]; `c ≤ 0.0031308 ? 12.92c : 1.055·c^(1/2.4) − 0.055`. Вектор: `color.json → srgb_to_linear` (допуск 1e-6).
**FN-L-02 `exposure(rgb, ev)`**: для каждого канала `linearToSrgb(srgbToLinear(c) × 2^ev)`, округление до 6 знаков. Вектор: `exposure` (6 кейсов, включая `ev=±5`, где результат прижимается).
**FN-L-03 `cdl(rgb, slope, offset, power, sat)`**: для каждого канала `v = max(c·slope + offset, 0)^power`; `y = 0.2126·v_r + 0.7152·v_g + 0.0722·v_b`; `out = y + sat·(v − y)`; зажать в [0;1]; округлить до 6 знаков. Вектор: `cdl` (7 кейсов).
**FN-L-04 `pchipCurve(points, nodes=4096)`**: монотонная кубическая интерполяция PCHIP (Фрицш—Карлсон) по точкам `(x,y)`, `x` строго возрастает; вне диапазона — ошибка `CURVE_RANGE`. Вектор: `color.json → pchip` (допуск 1e-6; сравнивать с `scipy.interpolate.PchipInterpolator`).
**FN-L-05 `buildLut(size, transform) → float32[size³][3]`**: порядок узлов: `for b in 0..N−1: for g: for r:` значение `transform(r/(N−1), g/(N−1), b/(N−1))`; индекс узла `r + N·g + N²·b`. Вектор: `color.json → lut` (размер 3, преобразование «CDL slope 0.5», 6 контрольных узлов).
**FN-L-06 `parseCube(text) → Lut3D`**: допускает `TITLE`, `DOMAIN_MIN`, `DOMAIN_MAX`, `LUT_3D_SIZE N` (17…65), затем ровно `N³` строк по три числа; отклоняет: размер вне диапазона, число строк ≠ `N³`, значение не конечное или вне `[domain_min; domain_max]`, `LUT_1D_SIZE`, длина файла > `LUT_MAX_BYTES`, строка > 256 символов. Ошибка → `LUT_INVALID` (на уровне API `VIDEO_UNSUPPORTED_FORMAT`).
**FN-L-07 `writeCube(lut) → text`**: заголовок `LUT_3D_SIZE N`, затем `N³` строк `%.6f %.6f %.6f` в порядке FN-L-05.
**FN-L-08 `foldEffectsToLut(effects, size=65) → Lut3D`**: порядок свёртки `basic → cdl → curves → lut3d`, затем по порядку списка для повторов; тождественная LUT при пустом списке; `lut3d.intensity` смешивает с тождеством линейно.
**FN-L-09 `rgbToLab(rgb)`**: sRGB → линейный → XYZ (матрица sRGB D65: `X = 0.4124564R + 0.3575761G + 0.1804375B`, `Y = 0.2126729R + 0.7151522G + 0.0721750B`, `Z = 0.0193339R + 0.1191920G + 0.9503041B`) → Lab с белой точкой `Xn=0.95047, Yn=1, Zn=1.08883`; `f(t) = t^(1/3)` при `t > 216/24389`, иначе `(24389/27·t + 16)/116`. Вектор: `lab`.
**FN-L-10 `deltaE76(a, b)`** = евклидово расстояние в Lab. Вектор: `delta_e76` (допуск 1e-3).

## Модуль R — рендер

**FN-R-01 `compileGraph(doc, presetId, runtime) → RenderPlan`** [Py `video/graph.py`] — чистая, детерминированная. Правила [07](07-timeline-edl-and-render-spec.md), раздел 6. Выход — DS-15; канонический JSON плана стабилен (golden-файл). Порядок входов `-i`: сначала видео- и фото-источники в порядке первого появления на дорожках `V1…V4`, затем аудио.
**FN-R-02 `videoChain(clip, fpsTarget, w, h) → str`**: цепочка фильтров клипа, точно такая: `trim=start=<src_in/TPS>:end=<src_out/TPS>,setpts=PTS-STARTPTS,fps=<fps>,scale=<w>:<h>:force_original_aspect_ratio=decrease,pad=<w>:<h>:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p`. Секунды выводятся как точная десятичная дробь из целых тиков (без `float`): `fmtSeconds(ticks)` = целая часть + до 9 знаков дроби без хвостовых нулей. Эталон: [`vectors/golden_render.md`](vectors/golden_render.md), графы G1/G2.
**FN-R-03 `audioChain(clip) → str`**: `atrim=start=…:end=…,asetpts=PTS-STARTPTS,aresample=48000[,volume=<gain_db>dB][,afade=t=in:st=0:d=<fade_in>][,afade=t=out:st=<…>:d=<fade_out>]`.
**FN-R-04 `crossfadeOffset(aLenTicks, durTicks) → ticks`** = `aLen − dur` (смещение `xfade` внутри потока клипа A, у которого в конце оставлен «запас» `dur/2` после точки склейки); для центрированного перехода клип A обрезается до `a.out + dur/2`, клип B начинается с `b.in − dur/2`. Эталон G2: A `0…3.5 с`, B `4.5…8 с`, `dur = 1 с`, `offset = 2.5`, итог **6.000 с, 150 кадров** при 25 к/с.
**FN-R-05 `renderFingerprint(...) → hex`**: `canonical_hash({document_sha256, preset_sha256, compiler_version, tool_versions, encoder, model_lock})`.
**FN-R-06 `qcVerify(path, expected) → QcReport`**: `ffprobe` длительность в пределах ±1 кадра от `expected_duration_ticks`; присутствуют ожидаемые потоки; полное декодирование `ffmpeg -v error -i f -f null -` без ошибок; размер ≤ `max_output_bytes`; интегральная громкость в пределах `±0.5 LU` от цели (только если цель задана).
**FN-R-07 `loudnormTwoPass(input, target, tp)`**: проход 1 `loudnorm=I=T:TP=tp:LRA=11:print_format=json` → `measured_*`; проход 2 с `measured_I/TP/LRA/thresh`, `offset`, `linear=true`. Эталон: синус 1 кГц −20 дБFS при цели −14 LUFS даёт `|output_i + 14| ≤ 0.5` (измерено: −14.05).

## Модуль W — операции воркера

Общий тип: `run(ctx: TaskContext, params: <DS-13>) → TaskResult` (в стиле существующих операций, образец — [`operations/media/derivative.py`](../../workers/photo_worker/operations/media/derivative.py)). Для каждой — запись реестра в `operations.json` (id, version `1`, package `VID1`, params, output, `resource_class`, `output_roles`, `timeout_seconds`, `max_output_bytes`, `role_policies`) **через генератор**, не вручную.

| ID | Операция | Вход → выход | Ключевые шаги |
|---|---|---|---|
| FN-W-01 | `video.probe` | оригинал → `video_probe` JSON (`ProbeResult`) | потоковый SHA-256 при чтении; `ffprobe -show_format -show_streams -of json`; отклонить по правилам DS-22 и лимитам; HDR → предупреждение |
| FN-W-02 | `video.proxy` | оригинал → `video_proxy` MP4 | CFR 25 к/с (если исходная частота кратна — сохранить), GOP = 1 с, H.264, AAC, `+faststart`, 540p/720p; JSON карты времени |
| FN-W-03 | `video.thumbs` | оригинал → `video_filmstrip` | спрайты 160×90, шаг `max(1 с, длительность/200)`; индекс JSON |
| FN-W-04 | `audio.waveform` | оригинал → `video_waveform` JSON | пики min/max по окнам; два разрешения (256 и 4096 точек на минуту) |
| FN-W-05 | `video.detect_cuts` | оригинал → `video_cut_report` (`CutReport`) | декод → признаки → модель/классика → `FN-C-01` |
| FN-W-06 | `video.color_analyze` | оригинал, диапазон → `video_color_report` | статистика кадров, предложение параметров `color.basic` |
| FN-W-07 | `video.preview_frames` | документ + замыкание → JPEG ≤ 16 | `FN-L-08` + кадр источника; sRGB, `q=90`, сторона ≤ 1280 |
| FN-W-08 | `audio.analyze` | аудио → отчёт JSON | BPM, сетка битов, интегральная громкость |
| FN-W-09 | `video.render` | документ + манифест + пресет → `video_render`, `video_qc_report` | `FN-R-01`, запуск ffmpeg, `FN-R-06`; отмена; лимит размера |
| FN-W-10 | `runFfmpeg(args, timeout, onProgress, maxBytes)` | — | запуск без оболочки (`subprocess` со списком аргументов), `-nostdin`, `-progress pipe:1`, ограничение рабочей директории, убийство дерева процессов по таймауту/отмене; разбор `out_time_us` |
| FN-W-12 | `video.lut_validate` | `.cube` (≤ `LUT_MAX_BYTES`) → `video_probe` JSON `LutValidateResult` | поток с лимитом размера → `parse_cube` (`FN-L-06`); любая ошибка → `VIDEO_UNSUPPORTED_FORMAT` (внутренний `LUT_INVALID`); класс cpu, пул `video_light`, таймаут 120 с; граф ingest для `kind=lut`: `video.lut_validate` → завершение (`FN-D-06`) |
| FN-W-11 | `ffmpegArgsSafe(args)` | — | белый список протоколов `-protocol_whitelist file,pipe`; запрещает `concat:`/`http`/`subfile`; применяется всегда |

## Модуль D — функции БД (`SECURITY DEFINER`, роль владельца `photo_fn_owner`)

Файлы миграций `02658`…`02662`. Общие правила: все аргументы проверяются на `NULL`/формат; возвращают `boolean` или `jsonb`-квитанцию; повтор с тем же идемпотентным ключом возвращает прежний результат.

| ID | Функция | Назначение | Роль EXECUTE |
|---|---|---|---|
| FN-D-01 | `photo.begin_video_upload(...)` | создать сессию, посчитать план (`Q-01…03`), создать резерв `video_upload` | `photo_api` |
| FN-D-02 | `photo.record_video_upload_part(...)` | записать ETag части (идемпотентно) | `photo_api` |
| FN-D-03 | `photo.finish_video_upload_completion(...)` | как `finish_upload_completion`: токен/lease, создание артефакта, медиа в `probing`, ingest-run | `photo_scheduler` |
| FN-D-04 | `photo.abort_video_upload(...)` | аборт, освобождение резерва | `photo_api` |
| FN-D-05 | `photo.expire_video_uploads(limit)` | пометка просроченных, постановка очистки | `photo_maintenance` |
| FN-D-06 | `photo.finish_video_ingest(...)` | одной транзакцией перевести медиа в `ready`/`rejected`, зафиксировать байты производных, закрыть резерв | `photo_scheduler` |
| FN-D-07 | `photo.save_video_sequence_revision(...)` | см. [04](04-data-model-and-api.md) 2.3; CAS по `head_revision` | `photo_api` |
| FN-D-08 | `photo.create_video_job(...)` | проверка capability, **допуск `FN-D-17`**, резерв `video_job`, run и outbox, запись `cost_seconds` (`FN-Q-09`) и `pool` — в одной транзакции | `photo_api` |
| FN-D-09 | `photo.finish_video_job(...)` | результат: `succeeded/failed/cancelled`, фактические байты, освобождение остатка | `photo_scheduler` |
| FN-D-10 | `photo.cancel_video_job(...)` | отмена; `VIDEO_JOB_NOT_CANCELLABLE` для терминальных | `photo_api` |
| FN-D-11 | `photo.create_video_render(...)` | рендер = задание + строка `video_renders` + резерв по `Q-04` | `photo_api` |
| FN-D-12 | `photo.finish_video_render(...)` | `verifying → ready/failed`, заполнение метаданных | `photo_scheduler` |
| FN-D-13 | `photo.withdraw_video_media(...)` / `photo.finish_video_media_delete(...)` | двухфазное удаление; квота освобождается только во второй | `photo_api` / `photo_maintenance` |
| FN-D-14 | `photo.create_video_delivery(...)`, `publish`, `unpublish`, `revoke`, `rotate_secret`, `extend` | управление выдачей поверх `collections` + `share_links` | `photo_api`/`photo_publisher` |
| FN-D-15 | `photo.record_video_egress(...)` | событие **скачивания** (`video_egress_events`); потоковый трафик учитывается `FN-D-19`/`FN-D-20` | `photo_portal` |
| FN-D-16 | `photo.compact_video_autosaves(...)` | GC автосохранённых ревизий | `photo_maintenance` |
| FN-D-17 | `photo.admit_video_job(tenant, kind, cost, ...)` | порядок 2–4 из [20](20-saas-scaling-operations.md) 3.3: лимит группы, суточный бюджет (upsert `video_tenant_usage_daily` — **только** при успехе всех проверок), порог перегрузки; вызывается из `FN-D-08`; повтор с тем же `Idempotency-Key` счётчики не меняет | вызывается внутри `FN-D-08` (отдельного GRANT нет) |
| FN-D-18 | `photo.set_video_tenant_limits(tenant, expected_revision, ...)` | запись переопределений лимитов (DS-29), CAS, аудит | `photo_admin` |
| FN-D-19 | `photo.lease_video_egress(tenant, share, want)` | арендовать бюджет трафика: `granted = min(want, остаток ссылки, остаток периода)`; увеличивает `leased_bytes` и создаёт строку `video_egress_leases` (`expires_at = now()+30 с`); `granted = 0` ⇒ `VIDEO_EGRESS_LIMIT` | `photo_portal` |
| FN-D-20 | `photo.settle_video_egress(tenant, lease, used_bytes, render, kind)` | вернуть остаток аренды, добавить `used` в `consumed`, upsert в `video_egress_counters`; повтор по тому же `lease` — no-op | `photo_portal` |
| FN-D-21 | `photo.purge_video_history(limit)` | очистка по [20](20-saas-scaling-operations.md) 5.4; создание месячных партиций `video_egress_events` на 3 месяца вперёд; батчи ≤ 1000, идемпотентно | `photo_maintenance` |
| FN-D-22 | `photo.reconcile_video_stuck(limit)` | согласователь ([20](20-saas-scaling-operations.md), раздел 8, п. 3); включает перевод просроченных аренд трафика в `consumed` | `photo_maintenance` |

## Модуль G — бэкенд Go (HTTP-слой)

| ID | Функция / обработчик | Файл | Примечание |
|---|---|---|---|
| FN-G-01 | `PlanUpload(bytes) (UploadPlan, error)` | `video/upload/plan.go` | обёртка `Q-01`/`Q-02`, тот же вектор |
| FN-G-02 | `SignParts(ctx, session, partNumbers) ([]PartURL, error)` | `video/upload/sign.go` | образец: [`objectstore/multipart.go`](../../backend/internal/objectstore/multipart.go); TTL 60 с |
| FN-G-03 | `Begin/Parts/Complete/Status/Abort` | `httpapi/video_upload.go` | образец: [`interchange_archive_upload.go`](../../backend/internal/httpapi/interchange_archive_upload.go) |
| FN-G-04 | `ListMedia/GetMedia/DeleteMedia/DerivativeLinks` | `httpapi/video_media.go` | курсор, `limit ≤ 100` |
| FN-G-05 | `StartCutDetection/StartColorAnalysis/StartAudioAnalysis` | `httpapi/video_jobs.go` | → `FN-D-08` |
| FN-G-06 | `CreateSequence/ListSequences/GetSequence/GetRevision/SaveRevision/Duplicate/Archive` | `httpapi/video_sequence.go` | валидатор Go (`FN-T-12`) до вызова `FN-D-07` |
| FN-G-07 | `ApplyCuts` | `httpapi/video_sequence.go`, `video/timeline/commands.go` | читает отчёт задания (проверяет `source_sha256`, иначе `VIDEO_JOB_STALE`), применяет Go-зеркало `FN-T-10` к голове монтажа при `expected_revision`, сохраняет ревизию с `label='cut_application'` через `FN-D-07` |
| FN-G-08 | `StartRender` | `httpapi/video_render.go` | проверка VT-10 и `FN-T-12(forRender)`, `FN-Q-04`, → `FN-D-11` |
| FN-G-09 | `GetJob/CancelJob/RetryJob/JobOutput` | `httpapi/video_jobs.go` | |
| FN-G-10 | `Renders list/get/delete/download-intent` | `httpapi/video_render.go` | |
| FN-G-11 | `CreateDelivery … ExtendDelivery`, `Stats` | `httpapi/video_delivery.go` | |
| FN-G-12 | `PortalVideoManifest/Stream/Download` | `portal/video.go` | Range-прокси: `parseRange(header, size) → (start, end, err)` — **чистая функция** с таблицей кейсов `TC-G-RNG-01…13` ([16](16-test-logic.md), раздел 4.5) |
| FN-G-13 | `Events SSE` | `httpapi/event_sse.go` (дополнить) | события DS-25 |
| FN-G-14 | `StreamLimiter` (потоки на сессию/ссылку, скорость, аренды трафика) | `portal/video_stream_limit.go` | `STREAMS_PER_SESSION`, `STREAMS_PER_SHARE`, `STREAM_MAX_BYTES_PER_SECOND`; аренда через `FN-D-19`, возврат через `FN-D-20` ≤ 30 с; превышение числа потоков → `VIDEO_STREAM_LIMIT`; режим `redirect` ([20](20-saas-scaling-operations.md), 6) — отдельная ветка с пессимистичным учётом |
| FN-G-15 | `PlaybackLinks` (`POST …/video-media/{id}/playback-links`, DS-31) | `httpapi/video_playback_links.go` | [23](23-editor-playback-timeline.md), раздел 15 |

## Модуль U — клиентская библиотека и хранилища (`apps/photographer/src`, `packages/api-client/src`)

| ID | Функция | Файл |
|---|---|---|
| FN-U-01 | `beginVideoUpload/requestPartUrls/completeVideoUpload/abortVideoUpload/getVideoUpload` | `packages/api-client/src/video-upload.ts` |
| FN-U-02 | `VideoUploadController` (очередь, состояния, возобновление) | `src/video-upload-controller.ts` |
| FN-U-03 | `VideoUploadAttemptStore` (по DS-26 + контрольные точки DS-11) | `src/video-upload-attempt-store.ts` |
| FN-U-04 | `readVideoMediaList/readVideoMedia` (разбор + проверка tenant/project, дубли, курсор ≤ 2048) | `src/video-media-list.ts` |
| FN-U-05 | `readCutReport(json, expectedMedia) → CutReport` (DS-12; инварианты) | `src/video-cut-report.ts` |
| FN-U-06 | `TimelineStore` (состояние, undo/redo, история команд, автосохранение с задержкой 1500 мс и при границах операций) | `src/video-timeline-store.ts` |
| FN-U-07 | `saveSequence(doc, expectedRevision)` с журналом попыток и разрешением конфликта | `src/video-sequence-save-attempt-store.ts` |
| FN-U-08 | `previewFrameCache(key=(sha,moment,size))` | `src/video-preview-frames.ts` |
| FN-U-09 | `parseCubeLut(text)` (зеркало `FN-L-06` на стороне клиента, ограниченное) | `src/video-lut-import.ts` |
| FN-U-10 | `useVideoCapabilities()` | `src/video-capabilities.ts` |
| FN-U-11 | `submitRender(intent)` | `src/video-render-submit.ts` |
| FN-U-12 | `watchVideoJob(jobId)` (SSE + опрос при потере потока) | `src/video-job-watch.ts` |
| FN-U-13 | `createDelivery(intent)` | `src/video-delivery-api.ts` |
| FN-U-14 | `autosaveGate(nowMs, lastSaveMs, dirty, minIntervalMs, force) → {action, waitMs}` (вектор `scale.json` → `autosave_gate`, 9 кейсов) | `src/video-autosave-gate.ts` |
| FN-U-15 | `PlaybackEngine` — исполнитель действий FN-E-14 (колоды, часы, слои, пересинхронизация) | `src/video-playback-engine.web.ts` |
| FN-U-16 | `AudioGraph` — граф Web Audio и планирование огибающих | `src/video-audio-graph.web.ts` |
| FN-U-17 | `getPlaybackLink(media, variant)` — запрос, разбор DS-31, кэш до `expires_at − 15 с` | `packages/api-client/src/video-playback-link.ts` |

## Модуль P — портал

| ID | Функция | Файл |
|---|---|---|
| FN-P-01 | `fetchVideoShare(shareId)` (манифест, ошибки `VIDEO_SHARE_*`) | `apps/portal/src/lib/video-share.ts` |
| FN-P-02 | `<VideoShareGallery>`, `<VideoPlayerCard>` | `apps/portal/src/components/Video*.tsx` |
| FN-P-03 | `requestDownload(renderId)` (намерение → `redeem`) | `apps/portal/src/lib/video-share.ts` |

## Модуль E — редактор: состав кадра, воспроизведение, таймлайн, цвет (добавлено, [23](23-editor-playback-timeline.md))

Все функции модуля чистые; нормативные определения — в указанных разделах 23; векторы — `editor.json`.

| ID | Функция | Файл | 23 | Векторы |
|---|---|---|---|---|
| FN-E-01 | `srcTicksAt(clip, t)` | `src/video-compose.ts`, Py `video/compose.py` | 3 | через `compose_at` |
| FN-E-02 | `effectiveTransition(doc, ti, ci, srcDur)` (общая для предпросмотра и рендера) | то же | 3 | `effective_transition` (10) |
| FN-E-03 | `photoTransformAt(clip, t)` | то же | 4 | `photo_transform` (8) |
| FN-E-04 | `composeAt(doc, t, srcDur)` | `src/video-compose.ts` | 4 | `compose_at` (19) |
| FN-E-05 | `audioEnvelope(doc, clipId, srcDur)` | то же | 5.1 | `audio_envelope` (6) |
| FN-E-06 | `scheduleEnvelope(points, t0, ctxTime0, rate)` | то же | 5.1 | `schedule_envelope` (4) |
| FN-E-07 | `tickToX`, `xToTick`, `layoutRows` | `src/video-timeline-geometry.ts` | 7 | `geometry` (11), `layout_rows*` |
| FN-E-08 | `hitTest(doc, view, x, y)` | то же | 7 | `hit_test` (25) |
| FN-E-09 | `zoomAt`, `zoomToFit`, `visibleClips` | то же | 7 | `zoom_at` (7), `zoom_to_fit` (4), `visible_clips` (4) |
| FN-E-10 | `snapAnchors`, `snapMove` | `src/video-timeline-snap.ts` | 8 | `snap_anchors` (2), `snap_move` (7) |
| FN-E-11 | `gestureReducer(state, event, ctx)`, `trimBounds`, `buildClip` | `src/video-timeline-gesture.ts` | 9 | `gestures` (22) |
| FN-E-12 | `matchEditorShortcut(event)` | `src/video-shortcuts.ts` | 10 | `shortcuts` (34) |
| FN-E-13 | `splitTarget`, `adjacentEdit` | то же | 10 | `split_target` (8), `adjacent_edit` (6) |
| FN-E-14 | `playbackReducer(state, event, ctx)` | `src/video-playback-state.ts` | 6 | `playback` (7 сценариев) |
| FN-E-15 | `wheelsToCdl`, `cdlToWheels` | `src/video-color-wheels-math.ts` | 14 | `wheels` (4) |
| FN-E-16 | `curveAdd`, `curveMove`, `curveRemove` | `src/video-color-curves-math.ts` | 14 | `curves` (9) |
| FN-E-17 | `scopes(width, height, rgb8)` | `src/video-scopes-math.ts` | 14 | `scopes` (2) |
| FN-E-18 | `copyClip(doc, selection)`, `pasteClip(doc, snap, playhead, newId)` | `src/video-clipboard.ts` | 20 | `copy_clip` (5), `paste_clip` (13), `paste_chain` (1) |
| FN-E-19 | `wheelStep(state, ev, view, end, busy)` | `src/video-timeline-wheel.ts` | 20 | `wheel` (18) |
| FN-E-20 | `formatTimecode(ticks, fps)` | `src/video-timecode.ts` | 20 | `timecode_format` (18), `timecode_roundtrip` (4) |
| FN-E-21 | `parseTimecode(text, fps)` | `src/video-timecode.ts` | 20 | `timecode_parse` (28), `timecode_roundtrip` (4) |
| FN-E-22 | `clipStyle(kind, flags)` | `src/video-timeline-style.ts` | 20 | `clip_style` (17) |

## Модуль X — лицензии и допуск компонентов (добавлено по запросу о лицензиях)

Нормативные определения — [22](22-tools-models-licenses.md), раздел 2.3; векторы — `license.json`.

| ID | Функция | Файл |
|---|---|---|
| FN-X-01 | `licenseVerdict(expr, kind) → {verdict, reasons[]}` (`kind ∈ code, weights, data`; вектор `license.json` → `license_verdict`, 48 кейсов) | `workers/photo_worker/models/license_policy.py` |
| FN-X-02 | `ffmpegBuildVerdict(buildconf, profile) → {ok, violations[]}` (`profile ∈ lgpl, gpl_internal`; вектор → `ffmpeg_build`, 20 кейсов) | там же |
| FN-X-03 | `modelApprovalGate(entry) → {eligible, reasons[]}` (вектор → `model_gate`, 14 кейсов) | там же |

## Модели (ML)

| ID | Модель / алгоритм | Роль | Статус выбора |
|---|---|---|---|
| ML-01 | TransNet V2: архитектура (код MIT) с **собственными весами** `video_boundary_transnet` (`VID-WP-18`); готовые веса из репозитория и портов не используются ([22](22-tools-models-licenses.md), D-18) | вероятности `single`, `many` по окнам из 100 кадров 48×27 | условный: доступен только при `approved_for_install` (FN-X-03); без него метод `model` недоступен |
| ML-02 | Классический детектор: гистограммные разности RGB 16 бинов на кадрах 48×27, пик над соседями ±2 кадра (резкие), сглаженное среднее с защитой от затемнений (плавные) — точные формулы в [15](15-task-cards.md), TK-407 | резерв на CPU; вычисляет `single`, `many`, `luma`; эталон проверен на трёх фикстурах | реализуется на `numpy`; не зависит от лицензий сторонних весов |
| ML-03 | Цвет-анализатор: баланс белого по серому миру, экспозиция по среднему `L*` | предложение `color.basic` | детерминированный алгоритм, без обучения |
| ML-04 | Биты/темп: onset-огибающая + автокорреляция | `audio.analyze` | детерминированный алгоритм на `numpy`/`scipy`; `librosa` в рабочий образ не входит ([22](22-tools-models-licenses.md), D-19); Beat This! — кандидат V2 |

Для ML-01/02 контракт интерфейса: `predict(frames: uint8[N,27,48,3]) → (single: float32[N−1], many: float32[N])`; `single[i]` относится к паре кадров `(i, i+1)`. Адаптеры `cuda`/`mps`/`mlx` реализуют один интерфейс `BoundaryModel` (методы `load(weights_path)`, `predict(frames)`); тест паритета: максимум абсолютной разности вероятностей между бэкендами ≤ `0.02` на фикстуре `hard_cuts.mp4`.
