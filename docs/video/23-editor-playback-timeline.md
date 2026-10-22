# 23. Редактор монтажа: воспроизведение, звук, таймлайн, команды, инспектор, цвет

Статус: спроектировано, не реализовано (2026-10-22). Нормативный документ. Начало: [README](README.md). Закрывает пробелы, найденные ревью пригодности для слабой модели ([21](21-review-report.md), раздел 7):

| Пробел | Раздел |
|---|---|
| нет функции «что на экране и что звучит в момент t» | 3, 4 |
| нет схемы микширования звука в Web Audio | 5 |
| нет автомата воспроизведения (предзагрузка, переключение, переходы, фото) | 6 |
| нет геометрии таймлайна, хит-теста, масштаба, автомата жестов и таблицы «жест → команда» | 7–9 |
| нет горячих клавиш для одиночных клавиш (существующий [`commands.ts`](../../apps/photographer/src/commands.ts) принимает только сочетания с Ctrl/Cmd) | 10 |
| половины команд редактора нет как функций с обратными операциями и векторами | 11, 12 |
| нет таблицы полей инспектора | 13 |
| нет алгоритмов колёс, кривых и осциллограмм | 14 |
| монитор не может читать proxy: `<video>` не передаёт заголовок `Authorization`, а приложение авторизуется именно им ([`packages/api-client/src/index.ts`](../../packages/api-client/src/index.ts)) | 15 |

Приоритет: схема документа монтажа — [07](07-timeline-edl-and-render-spec.md) и DS-09; поведение экранов V-06…V-08 — этот документ (при расхождении с [03](03-photographer-screens.md) действует 23). Все числа и ожидания — в `vectors/editor.json` (293 кейса), эталон — `vectors/reference/ref_editor_core.py`, `ref_editor_ui.py`, генератор `ref_editor.py` (только стандартная библиотека Python). Векторы проверяют соответствие этому тексту, а не правильность решений ([21](21-review-report.md), O-7).

Обозначения: `TPS = 705 600 000`; `FT` — тики кадра монтажа (FN-T-01); `dur(c)` — FN-T-04; `end(c) = c.at + dur(c)`; «деление» без уточнения — целочисленное с округлением **вниз** (для отрицательных чисел тоже вниз: в TS `Math.floor(a / b)`; все произведения < 2⁵³); дроби непрозрачности возвращаются как несократимые `{num, den}`.

## 1. Решения

| ID | Решение | Почему |
|---|---|---|
| D-24 | Специализированные поверхности редактора (таймлайн, монитор, колёса, кривые, осциллограммы) — **только web**: файлы `*.web.tsx`, DOM-элементы `<canvas>`, `<video>`, `<audio>`, `<img>`, события указателя (`pointerdown/move/up/cancel`). Парный файл `*.tsx` для нативных сборок показывает `CapabilityNotice` с причиной `platform_unsupported`. Новых зависимостей нет: ни `expo-video`, ни SVG-библиотеки, ни `react-native-gesture-handler`. | V1 редактора — web ([03](03-photographer-screens.md), правило 7); сборка уже различает `.web.tsx` ([`vitest.config.mjs`](../../apps/photographer/vitest.config.mjs)); политика Paper касается только импортов из `react-native` ([`paper-ui-policy.test.ts`](../../apps/photographer/src/paper-ui-policy.test.ts)). Снимает допущение A-10 для монитора. |
| D-25 | Воспроизведение: на каждую дорожку две «колоды» (медиа-элемента) `<track>/a` и `<track>/b`; часы — `AudioContext.currentTime`; логика — чистый автомат FN-E-14, который выдаёт список действий; исполняет их движок FN-U-15. | Переход между клипами без паузы требует заранее загруженного второго элемента; кроссфейд требует двух одновременно. |
| D-26 | Ссылки для воспроизведения владельцем получаются JSON-запросом `POST …/video-media/{id}/playback-links` (Bearer) и живут ≤ 60 с; при перемотке колоды, чья ссылка старше 45 с, ссылка обновляется; при ошибке чтения — повтор (до 3 раз). | `<video src>` не может передать `Authorization`; инвариант «подписанная ссылка ≤ 60 с» сохраняется. |
| D-27 | Предпросмотр в мониторе **приблизительный**: без цветовых эффектов, без ducking и нормализации громкости. На паузе монитор через 400 мс запрашивает точный кадр `video.preview_frames` (FN-U-08). Аудиоисточник без proxy звучит, только если его кодек есть в `AUDIO_PREVIEW_CODECS`; иначе клип в предпросмотре беззвучен с пометкой. | Точный предпросмотр уже существует (кадры воркера); полноценный движок эффектов в браузере — V2 ([03](03-photographer-screens.md), V-07). |
| D-28 | Правила сокращения перехода (FN-E-02) — **одни** для предпросмотра и рендера: компилятор графа (TK-413) берёт длительность и границы перехода из FN-E-02. Звуковой кроссфейд — **линейный** (`acrossfade c1=tri c2=tri`), как в эталоне G2 ([`golden_render.md`](vectors/golden_render.md)); фраза «равной мощности» в [07](07-timeline-edl-and-render-spec.md) заменена. | Без общего правила предпросмотр и рендер расходятся на кадры; G2 проверен на ffmpeg и уже является эталоном. |
| D-29 | Сглаживание движения фото `ease_in_out` = `3u² − 2u³` (FN-E-03), одно в TS и Python. | В 07 было «кубическая функция с фиксированными коэффициентами» без коэффициентов. |
| D-30 | Отмена и возврат в хранилище таймлайна — **снимками документа** (`past` ≤ 200 снимков и ≤ 32 МиБ канонических байтов; при превышении удаляются самые старые). Команды по-прежнему возвращают обратную команду, и её правильность проверяется векторами (VID-T-04). | Снимок даёт побайтно точную отмену при любых правках, в том числе от сервера (`cut-applications`); слабой модели проще и безопаснее. |
| D-31 | Набор команд редактора — FN-T-05…10 и FN-T-15…27; перед применением любой команды хранилище вызывает FN-T-28 (заблокированные дорожки). **Вне V1** (явно): обрезка со сдвигом (ripple trim), множественное выделение и рамка, затемнение в конце последнего клипа (нужно поле `transition_out`), обратное воспроизведение. | Ограничивает объём; каждое исключение видно пользователю (кнопки нет). |
| D-32 | Горячие клавиши редактора — собственная функция FN-E-12 и хук `useEditorShortcuts`; существующие `commands.ts`/`use-commands.ts` не меняются. | Существующий сопоставитель требует Ctrl/Cmd. |

## 2. Константы (дополнение DS-01)

| Имя | Значение | Где |
|---|---|---|
| `RULER_H` / `VIDEO_TRACK_H` / `AUDIO_TRACK_H` / `TRACK_GAP` / `HEADER_W` | `28` / `64` / `48` / `2` / `160` px | геометрия (FN-E-07) |
| `EDGE_PX` / `TRANSITION_HANDLE_PX` / `DRAG_THRESHOLD_PX` | `6` / `10` / `4` px | хит-тест, жесты |
| `SNAP_PX` | `8` px (уже в DS-01) | привязка |
| `ZOOM_LADDER` (тики на пиксель) | `705600, 1411200, 3528000, 7056000, 14112000, 35280000, 70560000, 141120000, 352800000, 705600000, 1411200000, 3528000000, 7056000000` (1 мс … 10 с на пиксель) | масштаб |
| `PHOTO_DEFAULT_TICKS` | `3528000000` (5 с) | фото из бина |
| `PRELOAD_LEAD_TICKS` | `1411200000` (2 с) | предзагрузка |
| `LINK_REFRESH_AGE_MS` / `DECK_MAX_FAILURES` | `45000` / `3` | воспроизведение |
| `DRIFT_MAX_MS` | `120` | движок: пересинхронизация видео |
| `PREVIEW_FRAME_DEBOUNCE_MS` | `400` | точный кадр на паузе |
| `UNDO_MAX_STEPS` / `UNDO_MAX_BYTES` | `200` / `33554432` | хранилище |
| `EFFECTS_PER_CLIP_MAX` | `16` | FN-T-18 |
| `MARKER_LABEL_MAX` | `200` | FN-T-25/26 |
| `AUDIO_PREVIEW_CODECS` | `aac, mp3, opus, vorbis, flac, pcm_s16le, pcm_s24le, pcm_f32le` | монитор (D-27) |
| `PLAYBACK_LINK_TTL_SECONDS` | `60` | D-26 |

## 3. Время источника и эффективный переход

**FN-E-01 `srcTicksAt(clip, t) → int | null`** [TS `video-compose.ts`, Py `video/compose.py`]. Фото → `null`. Иначе `src_in + ((t − at) × num) div den`, где `speed = num/den` (по умолчанию 1/1), `div` — деление вниз. `t` может быть меньше `at` (запасные кадры перед `src_in` для перехода). Векторы: через `compose_at`.

**FN-E-02 `effectiveTransition(doc, trackIndex, clipIndex, sourceDurations) → Transition | null`** [TS, Py]. Возвращает фактический переход клипа `c` на видеодорожке; используется предпросмотром **и** компилятором рендера (D-28). Вектор `effective_transition` (`ET-01…10`).

1. Дорожка не видео, нет `transition_in` или `kind = cut` → `null`.
2. `D = transition_in.duration`; `prev` — предыдущий клип дорожки; «смежный» ⇔ `end(prev) = c.at`.
3. `crossfade` или `dip_to_black` без смежного `prev` → обрабатывается как `fade_in` с той же длительностью.
4. `fade_in`: `d = min(D, dur(c))`; `d ≤ 0` → `null`; результат `{kind: fade_in, start: c.at, end: c.at + d, duration: d}`.
5. `dip_to_black` (всегда по центру, `align` не учитывается): `half = D div 2`; `{start: c.at − half, end: c.at + half, duration: 2·half}`. Запасные кадры не нужны: первая половина — `prev` в своём диапазоне, вторая — `c`.
6. `crossfade`: запас головы `head(c) = (c.src_in × den) div num` (фото: бесконечность); запас хвоста `tail(prev) = ((srcDur(prev) − prev.src_out) × den) div num` (фото: бесконечность).
   - `center`: `half = min(D div 2, head(c), tail(prev))`; `{start: c.at − half, end: c.at + half, duration: 2·half}`;
   - `start`: `d = min(D, tail(prev))`; `{start: c.at, end: c.at + d}`;
   - `end`: `d = min(D, head(c))`; `{start: c.at − d, end: c.at}`;
   - если длительность получилась 0 → `{kind: cut, duration: 0, shortened: true}`.
7. В результате всегда есть `clip`, `prev` (или `null`), `kind`, `start`, `end`, `duration`, `requested = D`, `shortened = duration < D`. Округления к кадру **нет**: центрированный переход 1 с при 25 к/с занимает ровно 0,5 с с каждой стороны, как в эталоне G2.

Рендер: компилятор (FN-R-01/04) использует `start`, `end`, `duration` из FN-E-02; при `shortened = true` записывает в план рендера `{clip, requested, effective}` (VID-T-07), редактор показывает предупреждение.

## 4. Состав кадра

**FN-E-03 `photoTransformAt(clip, t) → {scale, center[2]}`** [TS, Py]. Нет `motion` → `{1, [0.5, 0.5]}`. Иначе `u = clamp(t − at, 0, duration) / duration` (число с плавающей точкой); `e = u` (`linear`) или `u·u·(3 − 2u)` (`ease_in_out`); `scale = from.scale + (to.scale − from.scale)·e`, центр — так же по координатам. Вектор `photo_transform` (`PT-01…08`, допуск 1e-9).

**FN-E-04 `composeAt(doc, t, sourceDurations) → Composition`** [TS]. Вектор `compose_at` (`CA-01…19`).

```ts
type Layer = { track: string; clip: string; kind: 'video'|'photo'; src_ticks: number|null;
               opacity: {num:number; den:number}; brightness: {num:number; den:number};
               photo?: {scale:number; center:[number,number]} };
type Voice = { track: string; clip: string; src_ticks: number; gain_db: number; env: number; muted: boolean };
type Composition = { t: number; end: number; layers: Layer[]; voices: Voice[]; over_limit: boolean };
```

Слои (снизу вверх): видеодорожки в порядке документа (`V1` — низ); дорожки с `visible = false` пропускаются. На дорожке:

1. Найти первый клип (по порядку), у которого `effectiveTransition` не `null`, не `cut` и `start ≤ t < end`:
   - `fade_in`: один слой клипа, `opacity = (t − start)/duration`;
   - `crossfade`: два слоя — `prev` (`opacity 1`), затем `c` (`opacity = (t − start)/duration`);
   - `dip_to_black`, `half = duration/2`: при `t < c.at` — слой `prev` с `brightness = (c.at − t)/half`, иначе слой `c` с `brightness = (t − c.at)/half`.
2. Иначе — клип с `at ≤ t < end`, `opacity = brightness = 1/1`.
3. Слой фото дополнительно несёт `photo = photoTransformAt(c, t)`; `src_ticks = srcTicksAt(c, t)`.

Голоса (звук): для каждого клипа с источником `video` или `audio` на любой дорожке, если `t` в его промежутке звучания `[s, e)`: `s = c.at`, `e = end(c)`; на видеодорожке `s` уменьшается до `start` собственного `crossfade`, `e` увеличивается до `end` перехода `crossfade` следующего клипа. Поля голоса: `gain_db = clip.audio.gain_db + (аудиодорожка ? track.gain_db : 0)`; `muted = clip.audio.mute или (аудиодорожка и track.muted)`; `env` — FN-E-05 в точке `t`, округлённая до 12 знаков. Видеоклип на скрытой дорожке звучит (скрытие влияет только на изображение).

`end` — наибольший `end(c)` по документу (0 для пустого); `over_limit = число слоёв > MAX_LAYERS_AT_POINT (8)`.

## 5. Звук

### 5.1 Огибающая голоса

**FN-E-05 `audioEnvelope(doc, clipId, sourceDurations) → {clip, start, end, points[[tick, value]]}`** [TS]. Вектор `audio_envelope` (`AE-01…06`). Значение огибающей `f(t)` на промежутке звучания — **произведение** множителей:

| Множитель | Где действует | Значение |
|---|---|---|
| нарастание клипа | `t − c.at < fade_in` | `max(t − c.at, 0)/fade_in` |
| затухание клипа | `end(c) − t < fade_out` | `max(end(c) − t, 0)/fade_out` |
| собственный `crossfade` (видеодорожка) | `start ≤ t < end` | `(t − start)/duration` |
| собственный `dip_to_black` | то же | `0` при `t < c.at`, иначе `(t − c.at)/(duration/2)` |
| `crossfade` следующего клипа | его `start ≤ t < end` | `(end − t)/duration` |
| `dip_to_black` следующего | то же | `0` при `t ≥ next.at`, иначе `(next.at − t)/(duration/2)` |

Точки: начало и конец промежутка, начала и концы нарастания/затухания, начала, концы и середины (`c.at`) переходов — уникальные, по возрастанию, в пределах `[start, end]`. Значение в правой границе — предел слева: `0`, если там кончается затухание клипа, `crossfade` следующего или клип перед `dip_to_black`, иначе `1`. Между точками Web Audio интерполирует линейно; там, где одновременно действуют два множителя, это приближение (D-27).

**FN-E-06 `scheduleEnvelope(points, t0, ctxTime0, rate) → [[ctxTime, value]]`** [TS]. Точки до `t0` пропускаются; если `t0` попал внутрь отрезка, первой ставится точка `[ctxTime0, линейная интерполяция]`; остальные — `ctxTime0 + (tick − t0)/TPS/rate` (округление до 9 знаков). Если все точки раньше `t0` — одна точка с последним значением. Вектор `schedule_envelope` (`SE-01…04`).

### 5.2 Граф Web Audio (FN-U-16, `video-audio-graph.web.ts`)

```
колода (HTMLMediaElement) → MediaElementAudioSourceNode → GainNode «огибающая» → GainNode «дорожка» → GainNode «мастер» → destination
```

1. Один `AudioContext` на экран редактора; создаётся при первом «Пуск» (требование браузера к жесту пользователя).
2. Каждая колода, у клипа которой есть звук, подключается один раз (`createMediaElementSource` нельзя вызывать дважды для одного элемента); элемент **не** выводит звук сам (`muted = false`, но маршрут только через граф).
3. «Огибающая» колоды: при действиях `audio_sync`, `load`, `clock_start` движок вызывает `cancelScheduledValues(now)`, `setValueAtTime(текущее значение, now)`, затем `linearRampToValueAtTime` по точкам FN-E-06 для клипа колоды. Множитель уровня `10^(gain_db/20)`; `muted` → 0. Громкость дорожки (`track.gain_db`) уже входит в `gain_db` голоса, поэтому узел «дорожка» держит 1 и нужен только для заглушения дорожки на лету без перепланирования.
4. Скорость клипа: `playbackRate = speed × rate`, `preservesPitch = true` (аналог `atempo`).
5. Ducking (`audio.duck`) и мастер-громкость в предпросмотре **не** применяются (метка «Приблизительно»).
6. Аудиоисточник (вид `audio`) читается из оригинала по ссылке D-26, если кодек из probe входит в `AUDIO_PREVIEW_CODECS`; иначе колода не загружается, клип помечается значком «звук только в рендере».

## 6. Автомат воспроизведения

**FN-E-14 `playbackReducer(state, event, ctx{doc, sourceDurations}) → {state, effects[]}`** [TS `video-playback-state.ts`]. Чистая функция; время приходит в событиях (`now`, мс). Вектор `playback` (`PB-01…07`: последовательности событий, после каждого — действия и сводка состояния).

Состояние: `mode ∈ {paused, playing, stalled, ended}`, `t`, `rate ∈ {1,2,4}`, `loop: [in, out] | null`, `decks: {[deckId]: {clip, ready, failed, failures, link_ms}}`, `active: deckId[]` (по возрастанию), `layers` (последний отправленный набор). Начальное: `paused`, `t = 0`, `rate = 1`, колоды пусты.

**Нужные клипы дорожки в момент t** (`trackNeeds`): клип нужен, если его промежуток `[s, e)` (как у голоса в разделе 4; для фото — `[at, end)` с тем же расширением `crossfade`) содержит `t` («активный») или `t < s ≤ t + PRELOAD_LEAD_TICKS` («следующий»). Порядок: активные по `s`, затем следующие по `s`; берутся первые **два**. На скрытой видеодорожке фото не нужны (у них нет звука).

**Согласование `reconcile`** (выполняется во всех событиях, кроме `deck_ready`, `rate`, `loop`):

1. Колоды дорожек, которых больше нет в документе, удаляются: действие `unload`.
2. Для каждой дорожки и каждого нужного клипа, которого нет ни в одной колоде дорожки: колода — первая свободная (`a`, затем `b`), иначе первая, чей клип не нужен. Действие `load {deck, clip, src_ticks = srcTicksAt(clip, max(t, s))}`; колода становится `{ready: false, failed: false, failures: 0, link_ms: now}`.
3. `active` — колоды активных клипов. Слои `composeAt(t)` переводятся в `[{deck, z, opacity, brightness, photo?}]` (`z` — порядковый номер слоя); если набор отличается от `state.layers`, действие `layers`.

**События:**

| Событие | Обработка |
|---|---|
| `play {now}` | если `t ≥ end` → `t = loop.in` или 0; `reconcile`; если все активные колоды готовы (`ready`, отказавшая считается готовой) — «старт»; иначе `mode = stalled` |
| «старт» | для активных колод с видео/звуком: `sync {deck, src_ticks}` и `play_media`; затем `audio_sync {t}`, `clock_start {t}`; `mode = playing` |
| `pause {now}` | при `playing/stalled`: `clock_stop`, `pause_media` активных; `mode = paused`; `reconcile` |
| `seek {t, now}` | `t` зажимается в `[0, end]`; при `playing/stalled`: `clock_stop`, `pause_media`; `reconcile`; для активных колод, загруженных раньше: если `now − link_ms > LINK_REFRESH_AGE_MS` → повторный `load` (новая ссылка), иначе `sync`; затем при `playing/stalled` — «старт», если все готовы, иначе `stalled`; из `ended` → `paused` |
| `tick {t, now}` | только при `playing`. Если петля и `t ≥ loop.out` → как `seek(loop.in)`. Если `t ≥ end` → `t = end`, `clock_stop`, `pause_media`, `mode = ended`. Иначе `reconcile`; колоды, переставшие быть активными, → `pause_media`; если какая-то активная не готова → `clock_stop`, `pause_media` активных, `mode = stalled`; иначе ставшие активными → `sync`, `play_media` |
| `deck_ready {deck}` | `ready = true`; если `stalled` и все активные готовы → «старт» |
| `deck_error {deck, now}` | `failures + 1`; при `failures ≤ DECK_MAX_FAILURES` → `load` (новая ссылка) с текущей позиции, `ready = false`, и при `playing` для активной колоды — `stalled`; иначе `failed = ready = true`, действие `notify {code: PLAYBACK_SOURCE_FAILED, clip}`, и из `stalled` — «старт», если остальные готовы (клип показывается чёрным с пометкой) |
| `rate {rate}` | допустимы 1, 2, 4; при изменении — действие `rate` |
| `loop {range}` | `range = [in, out]` или `null`; действий нет |
| `doc {now}` | документ изменился: `t = min(t, end)`; `reconcile`; `sync` активных |

Действия `sync`, `play_media`, `pause_media` выдаются только колодам с видео- или аудиоисточником (не фото). Порядок действий в ответе: `clock_stop, pause_media, unload, load, sync, layers, rate, play_media, audio_sync, clock_start, notify`, внутри вида — по `deck`; повторы удаляются.

**Движок FN-U-15 (`video-playback-engine.web.ts`) — исполнитель действий, не принимает решений:**

1. Колода — `<video playsinline preload="auto">` для видео, `<audio preload="auto">` для звука, `<img>` для фото; все колоды дорожки лежат в одном контейнере монитора; видимость и порядок задаёт только действие `layers` (`style.opacity = num/den`, `filter: brightness(num/den)`, `zIndex = z`, для фото — CSS `transform: scale(s) translate(...)` от центра `center`, вписывание по `fit`). Колоды вне `layers` скрыты (`visibility: hidden`), но продолжают загрузку.
2. `load`: получить ссылку (FN-U-17, раздел 15), `src = url`, после `loadedmetadata` выставить `currentTime = src_ticks/TPS`, после `seeked` (для `<img>` — `load`) отправить автомату `deck_ready`; ошибка элемента или 403/410 по сети → `deck_error`.
3. Часы: `clock_start {t}` запоминает `t0 = t`, `c0 = audioContext.currentTime`; позиция `t = t0 + round((currentTime − c0) × TPS × rate)`, затем к ближайшему кадру (`FT`). Цикл `requestAnimationFrame` отправляет `tick {t, now}` не чаще раза за кадр экрана.
4. Пересинхронизация: на каждом `tick` для каждой активной видео/аудиоколоды ожидаемое время источника `srcTicksAt(clip, t)`; если `|element.currentTime × TPS − ожидание| > DRIFT_MAX_MS × TPS / 1000` → `currentTime = ожидание/TPS`. Это единственное место, где движок меняет позицию без команды автомата.
5. На паузе (`mode = paused`) через `PREVIEW_FRAME_DEBOUNCE_MS` после последнего `seek` движок запрашивает точный кадр (FN-U-08, `video.preview_frames`, существующий дебаунс TK-909) и показывает его поверх колод с подписью «Точный кадр»; любое действие `load/sync/clock_start` убирает его.
6. При `over_limit` монитор показывает «Слоёв больше 8 — предпросмотр упрощён» и не отображает слои выше восьмого.

## 7. Геометрия таймлайна

**FN-E-07** [TS `video-timeline-geometry.ts`]. Вид `view = {scroll, tpp, width}`: `scroll` — тик у левого края области клипов; `tpp` — тиков на пиксель; `width` — ширина области клипов в px.

- `tickToX(t) = HEADER_W + (t − scroll) div tpp`; `xToTick(x) = scroll + (x − HEADER_W) × tpp`.
- `layoutRows(doc)`: строки сверху вниз — видеодорожки в **обратном** порядке документа (верхняя `V4` сверху, `V1` над звуком), затем аудиодорожки в порядке документа. `y` первой = `RULER_H`; высота `VIDEO_TRACK_H` или `AUDIO_TRACK_H`; следующая `y = y + h + TRACK_GAP`. Каждая строка: `{track, kind, y, h, locked}`. Векторы: `layout_rows`, `layout_rows_four_video`, `geometry`.

**FN-E-08 `hitTest(doc, view, x, y) → Hit`** — порядок проверок (вектор `hit_test`, `HT-01…25`):

1. `x < 0` или `y < 0` → `none`.
2. `y < RULER_H`: `x ≥ HEADER_W` → `{ruler, tick: xToTick(x)}`, иначе `none`.
3. Строка по `y` (`row.y ≤ y < row.y + h`); нет → `none` (промежутки между строками).
4. `x < HEADER_W` → `{track_header, track}`.
5. Верхняя треть строки (`y < row.y + h div 3`): клип с переходом (не `cut`) и `|x − tickToX(c.at)| ≤ TRANSITION_HANDLE_PX div 2` → `{transition, clip}`.
6. Клип с `x0 ≤ x < x1` (`x0 = tickToX(c.at)`, `x1 = tickToX(end(c))`): `e = min(EDGE_PX, (x1 − x0) div 3)`; `x < x0 + e` и не фото → `edge_in`; `x ≥ x1 − e` → `edge_out`; иначе `clip_body`.
7. Иначе `{empty, tick}`.

Результаты 5–7 содержат `track` и `locked`.

**FN-E-09 масштаб и видимая область:**
- `zoomAt(view, anchorX, direction)`: `i` — индекс первого значения лестницы `≥ tpp`; `direction = +1` (приблизить) → `i − 1`, `−1` → `i + 1`, с зажатием в лестницу; тик под курсором сохраняется: `scroll = max(0, xToTick(anchorX) − (anchorX − HEADER_W) × newTpp)`. Колесо мыши с Ctrl/Cmd и клавиши `+`/`−` (якорь — playhead) вызывают его.
- `zoomToFit(doc, width)`: наименьшее `tpp` лестницы с `end ≤ tpp × width`; `scroll = 0`.
- `visibleClips(doc, view)`: клипы, пересекающие `[scroll − width·tpp, scroll + 2·width·tpp)` (запас в экран с каждой стороны), в порядке документа. Только они рисуются. Векторы: `zoom_at`, `zoom_to_fit`, `visible_clips`.

**Отрисовка** (`video-timeline-canvas.web.tsx`, один `<canvas>` на всю область, перерисовка в `requestAnimationFrame` при изменении документа, вида, выделения, playhead или предпросмотра жеста):

| Элемент | Как рисуется |
|---|---|
| линейка | шаг подписей — наименьший из ряда «1 кадр, 5 кадров, 1, 5, 10, 30, 60, 300, 600, 1800, 3600 с», при котором между подписями ≥ 60 px; подпись `чч:мм:сс:кк` |
| заголовки дорожек | имя, кнопки Paper `IconButton` поверх canvas: замок, глаз (видео), динамик и `RangeControl` громкости (звук) → FN-T-22 |
| клип | прямоугольник строки с отступом 2 px; видео — тайлы раскадровки (FN-U-04 артефакт `video_filmstrip`, тайл на каждые 160 px), аудио и звук видео — волна (`video_waveform`, пики по пикселю); фото — уменьшенное превью; имя источника; значок «звук только в рендере» (D-27); пометка «источник отсутствует» |
| переход | треугольник высотой треть строки по центру `tickToX(c.at)`; при `shortened` — жёлтый |
| выделение | рамка 2 px цвета `primary` темы Paper |
| playhead | вертикальная линия через все строки |
| маркеры | флажки на линейке (`cut` — красный, `beat` — синий, `user` — цвет темы) |
| предпросмотр жеста | полупрозрачная копия клипа в позиции `preview`; при `valid = false` — красная |
| привязка | вертикальная пунктирная линия на якоре, если позиция совпала с якорем |

Доступность ([03](03-photographer-screens.md), правило 8): рядом с canvas рендерится скрытый список (`role="list"`) клипов в порядке строк: «V1, клип c2, 00:00:04:00, длительность 4 с».

## 8. Привязка

**FN-E-10** [TS `video-timeline-snap.ts`, дополнение TK-306]:
- `snapAnchors(doc, playhead, exclude?)` = отсортированные уникальные `{0, playhead} ∪ {c.at, end(c)} (кроме клипа exclude) ∪ {marker.at}`.
- `snapTime` — FN-T-13 (ближайший якорь в пределах порога, при равенстве меньший; иначе округление к кадру `(t + FT div 2) div FT × FT`).
- `snapMove(raw, d, anchors, thr)`: `s1` — ближайший якорь к началу `raw`, `s2` — к концу `raw + d` (оба в пределах `thr`); если есть `s1` и (`s2` нет или `|s1 − raw| ≤ |s2 − (raw + d)|`) → `s1`; иначе если есть `s2` → `s2 − d`; иначе округление `raw` к кадру.
- Порог `thr = SNAP_PX × tpp`. Привязка выключается глобально клавишей `N` и временно — удержанием Shift во время перетаскивания. Векторы: `snap_anchors`, `snap_move`.

## 9. Автомат жестов

**FN-E-11 `gestureReducer(state, event, ctx) → {state, out}`** [TS `video-timeline-gesture.ts`]. `ctx = {doc, view, playhead, tool ∈ {select, razor}, snap, sourceDurations, newIds}`. События: `down {x, y, shift, alt}`, `move {x, y, shift}`, `up {x, y, alt}`, `cancel`, `bin_over {x, y, item}`, `bin_drop {x, y, item, alt}`, `bin_leave`. Выход: `{mode, preview|null, command|null, selection ('keep'|null|clipId), playhead|null}`. Вектор `gestures` (`GS-01…22`: последовательности событий и выходы).

Состояния: `idle`, `pressed`, `move`, `trim`, `transition`, `scrub`, `drop`.

| Состояние · событие | Условие | Результат |
|---|---|---|
| `idle` · `down` на линейке | — | `scrub`; `playhead` = тик, округлённый к кадру и зажатый в `[0, end]` |
| `idle` · `down` на теле клипа, инструмент «бритва» | дорожка не заблокирована | команда `split_clip {clip, at: snapTime(тик), new_id: newIds[0]}`; выделение = клип |
| `idle` · `down` на клипе (тело/край/ручка перехода) | — | `pressed`; выделение = клип; запоминаются `x0, y0`, вид цели, `locked` |
| `idle` · `down` на пустом месте | — | выделение снимается (`null`) |
| `pressed` · `move` | `locked` или смещение < `DRAG_THRESHOLD_PX` по обеим осям | остаётся `pressed` |
| `pressed` · `move` | иначе | тело → `move` (`grab = xToTick(x0) − c.at`); край → `trim`; ручка → `transition`; сразу считается предпросмотр |
| `pressed` · `up` | — | `idle` (это был клик) |
| `move` · `move` | — | `raw = xToTick(x) − grab`; дорожка — строка под курсором того же вида и не заблокированная, иначе исходная; `at = snapMove(...)` или к кадру при Shift/выключенной привязке; `at ≥ 0`; `valid` = нет пересечения с другими клипами целевой дорожки; `preview {clip, at, track, valid}` |
| `move` · `up` | `valid` и позиция или дорожка изменились | команда `move_clip {clip, new_at, track?}` (`track` только при смене); иначе без команды (возврат на место) |
| `trim` · `move` | — | `pos = snapTime(xToTick(x))` (якоря без самого клипа) или к кадру; зажать в `trimBounds`; видео/звук → `preview {clip, edge, pos}`; фото → `preview {clip, duration: pos − at}` |
| `trim` · `up` | значение изменилось | видео/звук → `trim_edge {clip, edge, pos, source_duration}`; фото → `set_clip_props {clip, patch: {duration}}` |
| `transition` · `move` | — | `lim` = предел FN-T-17, вниз к кадру; `D = max(min(2·FT, lim), min(lim, 2 × кадр(|xToTick(x) − c.at|)))`; `preview {clip, duration: D}` |
| `transition` · `up` | `D` изменилась | `set_transition {clip, transition: {kind, duration: D, align}}` (вид и выравнивание прежние) |
| `scrub` · `move` / `up` | — | новый `playhead` / `idle` |
| любое · `cancel` (Escape, `pointercancel`) | — | `idle`, предпросмотр снят, команды нет |
| `idle`/`drop` · `bin_over` | — | `drop`; дорожка — строка под курсором, вид `audio` для аудио и `video` для видео и фото, не заблокирована, `x ≥ HEADER_W`; `at = snapTime` (без исключений); `preview {track, at, valid}` |
| `drop` · `bin_drop` | `valid` | команда `add_clip {track, clip: buildClip(item, at, newIds[0]), ripple: alt}` |
| `drop` · `bin_leave` | — | `idle` |

`trimBounds(track, clip, edge)` (включительно, кратно кадру): видео/звук, край `in`: `lo = max(конец предыдущего, ceil_к_кадру(c.at − (src_in × den) div num), 0)`, `hi = end(c) − FT`; край `out`: `lo = c.at + FT`, `hi = min(начало следующего, floor_к_кадру(c.at + ((srcDur − src_in) × den) div num))`; фото: `[c.at + FT, начало следующего или c.at + 3 ч]`.

`buildClip(item, at, id)`: видео — `{id, source{video, media_id, sha256}, at, src_in: 0, src_out: floor_к_кадру(duration), speed 1/1, fit: fit, audio{0, 0, 0, false}, effects: []}`; звук — то же без `fit`; фото — `{id, source{photo, asset_version_id, sha256}, at, duration: PHOTO_DEFAULT_TICKS, fit: fit, effects: []}`.

**Таблица «жест → команда»** (сводка для тестов и справки в интерфейсе):

| Жест | Модификатор | Команда |
|---|---|---|
| тащить тело клипа | — / Shift (без привязки) | `move_clip` (FN-T-07) |
| тащить тело на другую дорожку того же вида | — | `move_clip` с `track` |
| тащить левый/правый край видео или звука | — | `trim_edge` (FN-T-06) |
| тащить правый край фото | — | `set_clip_props {duration}` (FN-T-21) |
| тащить ручку перехода | — | `set_transition` (FN-T-17) |
| клик бритвой по клипу | — | `split_clip` (FN-T-05) |
| бросить источник из бина | — / Alt (со сдвигом) | `add_clip` (FN-T-15) |
| клик/протяжка по линейке | — | playhead (без команды) |
| двойной клик по клипу | — | открыть вкладку «Клип» инспектора (без команды) |
| правый клик по клипу | — | меню Paper `Menu`: Разрезать по playhead, Дублировать, Удалить, Удалить со сдвигом, Переход…, Цвет… |

Привязка к DOM (`video-timeline-canvas.web.tsx`): `pointerdown` вызывает `setPointerCapture`; координаты — `event.clientX/Y` минус `getBoundingClientRect()` canvas; правая кнопка мыши не порождает `down` (`event.button !== 0` → игнор, открывается меню); колесо без модификатора прокручивает по горизонтали (`scroll += deltaY × tpp`, не меньше 0); перетаскивание из бина — HTML Drag and Drop (`dragover` → `bin_over`, `drop` → `bin_drop`, `dragleave` → `bin_leave`), данные элемента — JSON в `dataTransfer` с типом `application/x-photoeditor-video-item`. Команда из выхода автомата отправляется в `TimelineStore.apply`; ошибка показывается Paper `Snackbar` с текстом по коду.

## 10. Горячие клавиши и цели команд

**FN-E-12 `matchEditorShortcut(event) → Action | null`** [TS `video-shortcuts.ts`], `event = {key, ctrl, meta, shift, alt, repeat, editing, composing}`; `editing` — фокус в поле ввода (то же правило, что в [`use-commands.ts`](../../apps/photographer/src/use-commands.ts)). Вектор `shortcuts` (`KB-01…34`). Порядок: `composing`, `editing` или `alt` → `null`. С Ctrl/Cmd: `z` → `undo`, `Shift+z` → `redo`, `y` → `redo`, остальное → `null` (оставить браузеру). Повтор (`repeat`) допустим только для стрелок влево/вправо и `+ = - _`. Таблица (регистр букв не важен):

| Клавиша | Действие | Поведение |
|---|---|---|
| Пробел | `play_toggle` | `play`/`pause` автомата |
| K / L / J | `pause` / `shuttle_forward` / `step_back_second` | пауза / скорость 1→2→4 / перемотка на 1 с назад |
| S | `split` | `split_clip` клипа из FN-E-13 в точке playhead |
| Delete / Backspace (+Shift) | `delete` (`delete_ripple`) | `delete_clip` выделенного, `ripple` при Shift |
| ← / → (+Shift) | `step_back_1`/`step_fwd_1` (`_10`) | playhead ± 1 (10) кадров, в пределах `[0, end]` |
| ↑ / ↓ | `prev_edit` / `next_edit` | playhead на ближайшую границу (FN-E-13) |
| + = / − _ / \ | `zoom_in` / `zoom_out` / `zoom_fit` | FN-E-09 (якорь — playhead) |
| I / O (+Shift) | `loop_in` / `loop_out` (`loop_clear_*`) | петля автомата (`loop`) |
| M | `add_marker` | `add_marker {id, at: playhead, kind: user, label: ''}`; `MARKER_EXISTS` игнорируется молча |
| C / V | `tool_razor` / `tool_select` | инструмент |
| N | `snap_toggle` | привязка вкл/выкл |
| Home / End | `go_start` / `go_end` | playhead 0 / `end` |
| Escape | `escape` | `cancel` жеста, иначе снять выделение |

**FN-E-13** [TS]: `splitTarget(doc, selection, playhead)` — выделенный клип, если `c.at < playhead < end(c)` и дорожка не заблокирована; иначе первый такой клип по строкам сверху вниз (FN-E-07), пропуская заблокированные; нет → `null` (кнопка «Разрезать» неактивна). `adjacentEdit(doc, playhead, dir)` — ближайшая граница из `{0, end} ∪ {c.at, end(c)}` строго меньше (`dir = −1`) или больше (`+1`) playhead; нет → playhead. Векторы: `split_target`, `adjacent_edit`.

## 11. Команды (дополнение модуля T)

Общие правила: все команды — чистые функции `(doc, args) → {ok: true, doc, inverse} | {ok: false, error}` [TS `video-timeline-commands.ts`]; документ не изменяется на месте; ошибки проверяются **в указанном порядке**, первая найденная возвращается; идентификаторы — шаблон `^[A-Za-z0-9_-]{1,64}$` (иначе `ID_INVALID`) и уникальны **по всему документу** среди дорожек, клипов, эффектов (включая мастер) и маркеров (иначе `ID_DUPLICATE`). Значение `null` в `patch` удаляет необязательное поле. Обратная команда (`inverse`) возвращается вместе с результатом и проверяется векторами: её применение к результату даёт исходный документ побайтно (канонически). Вектор `commands` (83 кейса; поле `doc` кейса указывает вариант из `docs`, по умолчанию `base`).

| ID | Команда | Ошибки (по порядку) | Результат | Обратная |
|---|---|---|---|---|
| FN-T-15 | `add_clip {track, clip, ripple}` | `TRACK_NOT_FOUND` · `ADD_TRACK_KIND` (вид клипа ≠ вид дорожки; вид клипа `audio` для источника `audio`, иначе `video`) · `ID_INVALID` (клип или его эффекты) · `ID_DUPLICATE` · `ADD_NOT_ALIGNED` (`at < 0`, `at mod FT ≠ 0` или `dur mod FT ≠ 0`) · `ADD_EMPTY` (`dur ≤ 0`) · `ADD_OVERLAP` (без сдвига: пересечение с любым клипом; со сдвигом: `at` строго внутри клипа) | со сдвигом все клипы дорожки с `at ≥ clip.at` сдвигаются на `dur(clip)`; клип добавлен, дорожка отсортирована по `at` | `delete_clip {clip, ripple}` |
| FN-T-16 | `duplicate_clip {clip, new_id}` | `CLIP_NOT_FOUND` · `ID_INVALID` · `ID_DUPLICATE` (`new_id` и `new_id-e{k}` эффектов) · `DUP_NO_SPACE` | копия сразу после клипа (`at = end(c)`), без `transition_in`, эффекты переименованы `new_id-e1…` | `delete_clip {new_id, false}` |
| FN-T-17 | `set_transition {clip, transition | null}` | `CLIP_NOT_FOUND` · `TRANSITION_TRACK_KIND` (не видеодорожка) · для не-`cut`: `TRANSITION_KIND` (только `crossfade`, `dip_to_black`, `fade_in`) · `TRANSITION_NOT_ALIGNED` (`duration ≤ 0` или не кратна `FT`) · `TRANSITION_ALIGN` (`align ∉ {center,start,end}`, или не `center` у не-`crossfade`) · `TRANSITION_FIRST_CLIP` (первый клип дорожки, вид не `fade_in`) · `TRANSITION_TOO_LONG` (больше `dur(c) div 2` для первого клипа, иначе `min(dur(c), dur(prev)) div 2`) | `null` или `cut` удаляют поле; иначе `transition_in = {kind, duration, align}` | `set_transition` с прежним значением |
| FN-T-18 | `set_effect {owner, effect, index?}` (`owner` — id клипа или `master`) | `OWNER_NOT_FOUND` · `EFFECT_TYPE` (не из списка создаваемых: `color.basic/cdl/curves/lut3d`, `audio.gain/fade/duck`; `color.match` создаётся только как `cdl`/`curves`) · `EFFECT_OWNER_KIND` (`audio.*` только у аудиоклипов; `color.*` только у видео, фото и мастера) · `EFFECT_PARAM` (ключи вне `id,type,enabled,params,meta`, `enabled` не логический, параметры вне таблицы 11.1) · `ID_INVALID` · `ID_DUPLICATE` (новый id есть где-то ещё) · `EFFECT_LIMIT` (16) · `EFFECT_INDEX` (`index ∉ [0, len]`) | эффект с тем же `id` у владельца заменяется на месте (`index` не учитывается); иначе вставляется по `index` (по умолчанию в конец) | замена → `set_effect` со старым; вставка → `remove_effect` |
| FN-T-19 | `remove_effect {owner, effect}` | `OWNER_NOT_FOUND` · `EFFECT_NOT_FOUND` | удалён | `set_effect {owner, effect: старый, index: прежний}` |
| FN-T-20 | `move_effect {owner, effect, index}` | `OWNER_NOT_FOUND` · `EFFECT_NOT_FOUND` · `EFFECT_INDEX` (`index ∉ [0, len−1]`) | перемещён | `move_effect` на прежний индекс |
| FN-T-21 | `set_clip_props {clip, patch}` | `CLIP_NOT_FOUND` · `PROPS_KEY` (ключ не разрешён для вида: видео — `fit, transform, audio`; фото — `fit, transform, motion, duration`; звук — `audio`; или неизвестный подключ) · `PROPS_RANGE` (таблица 11.2; `duration: null`) · `PROPS_RANGE` (`fade_in + fade_out > dur`) · `PROPS_OVERLAP` (фото налезает на следующий клип) | ключи применяются в алфавитном порядке; `transform` и `audio` **сливаются** с текущими (или значениями по умолчанию), `motion` заменяется целиком | `set_clip_props` с прежними **полными** значениями изменённых ключей (`null` для отсутствовавших) |
| FN-T-22 | `set_track_props {track, patch}` | `TRACK_NOT_FOUND` · `PROPS_KEY` (видео — `locked, visible`; звук — `locked, muted, gain_db`) · `PROPS_RANGE` (`gain_db ∉ [−60; 24]`, не логическое) | применено | `set_track_props` с прежними значениями (`null` для отсутствовавших) |
| FN-T-23 | `add_track {kind, track, index?, props?}` | `TRACK_KIND` · `ID_INVALID` · `ID_DUPLICATE` · `TRACK_LIMIT` (4 одного вида) · `TRACK_INDEX` (видеодорожки всегда перед аудио: видео — `[0, nV]`, звук — `[nV, nV + nA]`); `props` проверяются как FN-T-22 | пустая дорожка; по умолчанию — после последней дорожки того же вида | `remove_track` |
| FN-T-24 | `remove_track {track}` | `TRACK_NOT_FOUND` · `TRACK_NOT_EMPTY` · `TRACK_REFERENCED` (в `audio.duck`) · `TRACK_LAST` (последняя дорожка своего вида) | удалена | `add_track {kind, track, index, props?}` |
| FN-T-25 | `add_marker {marker}` | `MARKER_KIND` (ключи не ровно `id, at, kind, label`) · `ID_INVALID` · `ID_DUPLICATE` · `MARKER_NOT_ALIGNED` · `MARKER_KIND` (вид ∉ `cut, beat, user`) · `MARKER_LABEL` (> 200 символов) · `MARKER_EXISTS` (тот же `at` и `kind`) | добавлен; маркеры сортируются по `(at, id)` | `remove_marker` |
| FN-T-26 | `update_marker {marker, patch{at?, label?}}` | `MARKER_NOT_FOUND` · `PROPS_KEY` · `MARKER_NOT_ALIGNED` · `MARKER_LABEL` · `MARKER_EXISTS` | изменён, пересортирован | `update_marker` с прежними значениями |
| FN-T-27 | `remove_marker {marker}` | `MARKER_NOT_FOUND` | удалён | `add_marker` |
| FN-T-28 | `checkLocked(doc, command) → 'TRACK_LOCKED' | null` | — | `TRACK_LOCKED`, если команда меняет клип на заблокированной дорожке (`split, trim, delete, speed, duplicate, set_transition, set_clip_props, move` — исходная **и** целевая дорожка, `set/remove/move_effect` у клипа) или добавляет клип на неё (`add_clip`). Команды дорожек (`set_track_props` — чтобы можно было разблокировать), маркеров и эффектов мастера не проверяются. Вектор `check_locked` (`LK-01…09`) | — |

Коды ошибок команд — клиентские (показываются по ключу локализации `video_editor_error_<код>`), в DS-24 не входят: на сервер уходит документ, который проверяет валидатор (VT).

### 11.1 Параметры эффектов (дополнение VT-07; FN-T-12 проверяет те же диапазоны)

| Тип | Параметры |
|---|---|
| `color.basic` | любые из `exposure_ev ∈ [−5; 5]`, `temperature, tint, contrast, highlights, shadows, whites, blacks, saturation, vibrance ∈ [−100; 100]`; других ключей нет |
| `color.cdl` | ровно `slope[3] ∈ [0; 4]`, `offset[3] ∈ [−1; 1]`, `power[3] ∈ [0.1; 4]`, `saturation ∈ [0; 4]` |
| `color.curves` | непустое подмножество `master, r, g, b`; каждая кривая — 2…16 точек `[x, y]`, `x, y ∈ [0; 1]`, `x` строго возрастает |
| `color.lut3d` | ровно `lut {kind: 'lut', media_id, sha256}`, `intensity ∈ [0; 1]` |
| `audio.gain` | ровно `gain_db ∈ [−60; 24]` |
| `audio.fade` | ровно `fade_in, fade_out` — целые ≥ 0 |
| `audio.duck` | ровно `sidechain_tracks` (1…4 разных id существующих аудиодорожек), `threshold_db ∈ [−60; 0]`, `depth_db ∈ [0; 40]`, `attack_ms ∈ [1; 1000]`, `release_ms ∈ [10; 5000]` |

Числа — конечные, не логические. Редактор V1 не создаёт `audio.gain` и `audio.fade` (громкость и затухания клипа — поле `audio`, FN-T-21), но принимает их в документе.

### 11.2 Свойства клипа (дополнение VT-07)

| Поле | Диапазон |
|---|---|
| `fit` | `fit`, `fill`, `none` |
| `transform` | ровно `scale ∈ [0.1; 8]`, `position[2] ∈ [−1; 1]` (доля кадра), `rotate ∈ [−180; 180]`, `crop[4] ∈ [0; 0.45]`; по умолчанию `{1, [0,0], 0, [0,0,0,0]}` |
| `audio` | ровно `gain_db ∈ [−60; 24]`, `fade_in, fade_out` — целые ≥ 0, кратные `FT`, сумма ≤ `dur`, `mute` логическое; по умолчанию `{0, 0, 0, false}` |
| `motion` | `{kind: pan_zoom, from{scale ∈ [1; 4], center[2] ∈ [0; 1]}, to{…}, easing ∈ {linear, ease_in_out}}` |
| `duration` (фото) | целое, кратное `FT`, ≥ `FT` |

## 12. Хранилище таймлайна (дополнение FN-U-06, TK-505)

1. Состояние: `doc`, `past: TimelineDocument[]`, `future: TimelineDocument[]`, `selection: string | null`, `playhead`, `tool`, `snap`, `dirty`, `lastError`.
2. `apply(command)`: `checkLocked` → ошибка `TRACK_LOCKED` без изменений; применить команду; ошибка → `lastError`, состояние не меняется, `past` не растёт; успех → `past.push(старый doc)`, `future = []`, `doc = новый`, `dirty = true`; выделение сохраняется, если клип существует, иначе `null`; затем ограничение `past` (D-30).
3. `undo()`/`redo()` — перенос снимков между `past`, `doc`, `future`; `dirty = true`.
4. Новые идентификаторы для команд (`new_id`, `newIds`) выдаёт `nextId(prefix)` хранилища: `prefix + base36(счётчик)` с пропуском занятых; счётчик в состоянии, при загрузке документа — 1. Это делает тесты детерминированными.
5. Применение результата `cut-applications` с сервера — как одна команда (снимок до и после).
6. Автосохранение — без изменений (FN-U-14, TK-909).

## 13. Инспектор (V-06, правая панель)

Вкладки Paper `SegmentedButtons`; каждое поле — Paper `TextInput` (число) в паре с `RangeControl` (TK-600) или `Switch`/`Menu`. Изменение поля отправляет команду **при отпускании** ползунка или по Enter/потере фокуса в поле (не на каждое движение — один снимок отмены на одно изменение). Значение вне диапазона — поле подсвечено, команда не отправляется. На заблокированной дорожке все поля неактивны с подсказкой «Дорожка заблокирована».

| Вкладка | Поле | Тип / шаг | Диапазон | Команда |
|---|---|---|---|---|
| Клип | Начало на таймлайне | таймкод `чч:мм:сс:кк` | ≥ 0 | `move_clip {new_at}` |
| Клип | Вход / выход источника | таймкод | FN-E-11 `trimBounds` | `trim_edge` |
| Клип | Длительность (фото) | таймкод | ≥ 1 кадр | `set_clip_props {duration}` |
| Клип | Вписывание | `Menu`: вписать / заполнить / как есть | — | `set_clip_props {fit}` |
| Клип | Масштаб, позиция X/Y, поворот, обрезка Л/В/П/Н | число, шаг 0.01 / 0.01 / 1° / 0.01 | 11.2 | `set_clip_props {transform: {…}}` |
| Скорость | Скорость | `Menu`: 1/4, 1/2, 3/4, 1, 3/2, 2, 3, 4 | 07 §3.4 | `set_speed` (FN-T-09) |
| Переход | Вид | `Menu`: нет / растворение / через чёрное / появление | 11 (FN-T-17) | `set_transition` |
| Переход | Длительность | кадры, шаг 1 | до предела FN-T-17 | `set_transition` |
| Переход | Выравнивание (только растворение) | `SegmentedButtons`: центр / начало / конец | — | `set_transition` |
| Звук | Громкость клипа | дБ, шаг 0.5 | [−60; 24] | `set_clip_props {audio: {gain_db}}` |
| Звук | Нарастание / затухание | кадры | 11.2 | `set_clip_props {audio: {fade_in | fade_out}}` |
| Звук | Без звука | `Switch` | — | `set_clip_props {audio: {mute}}` |
| Звук (аудиоклип) | Приглушение под речь: дорожки, порог, глубина, атака, восстановление | `Menu` (мультивыбор) + числа | 11.1 | `set_effect {audio.duck}` / `remove_effect` |
| Движение (фото) | Включено; начало/конец: масштаб, центр X/Y; сглаживание | `Switch` + числа + `Menu` | 11.2 | `set_clip_props {motion}` / `{motion: null}` |
| Цвет | открывает V-07 для выделенного клипа | — | — | — |

Заголовок дорожки: замок, видимость, без звука, громкость — `set_track_props`. Меню «Дорожки»: «Добавить видеодорожку», «Добавить аудиодорожку» (`add_track`, id — `nextId('V')`/`nextId('A')`), «Удалить пустую дорожку» (`remove_track`). Бин (`video-bin.tsx`): список готовых медиа проекта (FN-U-04) и версий фото (существующий выбор активов), фильтр по виду, поиск по имени; элемент перетаскивается на таймлайн (раздел 9) или добавляется кнопкой «В конец V1/A1» (`add_clip` в `end` дорожки).

## 14. Цвет (V-07): колёса, кривые, осциллограммы

**FN-E-15 `wheelsToCdl(lift, gamma, gain) → {slope[3], offset[3], power[3]}` и `cdlToWheels(cdl)`** [TS `video-color-wheels-math.ts`]. Колесо — `{x, y, v}`: шайба в единичном круге (вектор длиннее 1 нормируется), `v` — кольцо-ползунок, зажимается в `[−1; 1]`. Для шайбы: `r = hypot(x, y)`, `θ = atan2(y, x)`, `d_c = r·cos(θ − φ_c)`, `φ = [0, 2π/3, 4π/3]` для R, G, B. Тогда `offset_c = clamp(0.1·v_lift + 0.1·d_lift_c, −1, 1)`; `power_c = clamp(2^(−(0.5·v_gamma + 0.5·d_gamma_c)), 0.1, 4)`; `slope_c = clamp(2^(v_gain + 0.5·d_gain_c), 0, 4)`. Обратное (для отображения сохранённого `color.cdl`): `a_c` = `offset_c/0.1`, `−log2(power_c)/0.5`, `log2(slope_c)` соответственно; `v = среднее(a)`; `d_c = (a_c − v)/k` (`k = 1` для lift и gamma, `0.5` для gain); `x = (2/3)·Σ d_c·cos φ_c`, `y = (2/3)·Σ d_c·sin φ_c`. Вне зажимов прямое и обратное взаимно обратны. `saturation` CDL — отдельный ползунок. Вектор `wheels` (`WC-01…04`, поле `roundtrip`; допуск 1e-9).

Поверхность (`video-color-wheels.web.tsx`): три круга по 160 px на `<canvas>` с цветовым кругом; перетаскивание шайбы меняет `x, y`; кольцо и парное `TextInput` — `v`; двойной клик — сброс. При отпускании отправляется `set_effect` с эффектом `color.cdl` клипа (id эффекта: существующий `color.cdl` клипа или `nextId(clipId + '-cdl')`).

**FN-E-16 правка кривых** [TS `video-color-curves-math.ts`]: `curveAdd(points, x, y)` — `x, y` зажимаются в `[0; 1]`; 16 точек → `CURVE_FULL`; `|x − x_i| < 1/256` для любой точки → `CURVE_X_TAKEN`; вставка по возрастанию, возвращается индекс. `curveMove(points, i, x, y)` — крайние точки двигаются только по `y`; внутренние `x` зажимаются в `(x_{i−1} + 1/256; x_{i+1} − 1/256)`; `y ∈ [0; 1]`; неверный индекс → `CURVE_INDEX`. `curveRemove(points, i)` — крайние → `CURVE_ENDPOINT`. Кривая по умолчанию `[[0,0],[1,1]]`. Вектор `curves` (`CV-01…09`). Поверхность `video-color-curves.web.tsx`: квадрат 256×256 px, клик по пустому месту — добавить, перетаскивание — двигать, двойной клик/Delete по точке — удалить; линия рисуется по FN-L-04 (PCHIP) с 256 узлами; вкладки Master/R/G/B.

**FN-E-17 `scopes(width, height, rgb8[]) → {histogram, waveform, vectorscope}`** [TS `video-scopes-math.ts`] — по превью-кадру (JPEG из `video.preview_frames`, декодированный в `ImageData`). Яркость `Y = floor(0.2126·R + 0.7152·G + 0.0722·B + 0.5)` (на гамма-кодированных значениях, как у привычных осциллограмм). Гистограмма — по `R, G, B, Y`, 256 корзин. Осциллограмма — `cols = min(width, 256)`, столбец `x·cols div width`, корзина `Y`. Векторскоп — `Cb = (−0.1146·R) + (−0.3854·G) + (0.5·B)`, `Cr = (0.5·R) + (−0.4542·G) + (−0.0458·B)` (порядок сложения именно такой), корзины `clamp(floor(Cb + 128 + 1e−9), 0, 255)` и то же для `Cr`. Выход — отсортированные ненулевые корзины. Вектор `scopes` (`SC-01…02`). Отрисовка — `video-scopes.web.tsx`, три `<canvas>` 256×256, логарифмическая яркость точки `log1p(count)/log1p(max)`.

Вкладка «LUT»: импорт `.cube` (FN-U-09), список LUT проекта, ползунок интенсивности → `set_effect {color.lut3d}`. Вкладка «Основное» — поля `color.basic` (11.1) → `set_effect {color.basic}`. «До/после» — переключатель (`Switch`): «до» — кадр proxy в мониторе (он всегда без эффектов, D-27), «после» — точный кадр `video.preview_frames`; отдельный запрос «без эффектов» не делается.

## 15. Ссылки для воспроизведения (API, D-26)

| Метод и путь | Назначение |
|---|---|
| `POST /projects/{p}/video-media/{id}/playback-links` | тело `{variant: 'proxy' | 'original_audio'}`; ответ `{url, expires_at, mime, codec}`; срок ≤ `PLAYBACK_LINK_TTL_SECONDS` |

Правила (FN-G-15, `httpapi/video_playback_links.go`): право `read` на проект; медиа того же tenant/project в состоянии `ready`, иначе 404 (одинаковый ответ «не найдено»); `proxy` — только для `kind = video`, `original_audio` — только для `kind = audio` и кодека из `AUDIO_PREVIEW_CODECS`; любое другое сочетание — `409 VIDEO_PLAYBACK_UNSUPPORTED`; подпись — существующий [`signed_read_url.go`](../../backend/internal/objectstore/signed_read_url.go) с повторной проверкой авторизации после подписи; трафик владельца не тарифицируется (D-15). Ограничение частоты — 120 запросов в минуту на пользователя (`429 VIDEO_TENANT_LIMIT / PLAYBACK_LINK_RATE`). Клиент (FN-U-17, `packages/api-client/src/video-playback-link.ts`): разбор ответа по образцу [`editor-source-preview-link.ts`](../../apps/photographer/src/editor-source-preview-link.ts) (`https` или локальный `http`, без учётных данных и фрагмента, `expires_at` в будущем), кэш по `(media, variant)` до `expires_at − 15 с`. Существующий `GET …/proxy` (307) остаётся для других потребителей. Ссылки на фото — существующий `preview1600` (`readEditorSourcePreview`).

## 16. Файлы

| Файл | Содержимое | Карточка |
|---|---|---|
| `apps/photographer/src/video-compose.ts` (+ `workers/photo_worker/video/compose.py` для FN-E-01…03) | FN-E-01…06 | TK-620, TK-634 |
| `src/video-timeline-commands.ts` (дополнение) | FN-T-15…28 | TK-621 |
| `src/video-timeline-store.ts` (дополнение) | раздел 12 | TK-622 |
| `src/video-timeline-geometry.ts`, `video-timeline-snap.ts` (дополнение) | FN-E-07…10 | TK-623 |
| `src/video-timeline-gesture.ts` | FN-E-11 | TK-624 |
| `src/video-shortcuts.ts`, `src/use-editor-shortcuts.ts` | FN-E-12, FN-E-13 | TK-625 |
| `src/video-playback-state.ts` | FN-E-14 | TK-626 |
| `src/video-playback-engine.web.ts`, `src/video-audio-graph.web.ts` | FN-U-15, FN-U-16 | TK-627 |
| `backend/internal/httpapi/video_playback_links.go`, `packages/api-client/src/video-playback-link.ts` | FN-G-15, FN-U-17 | TK-628 |
| `src/video-timeline-canvas.web.tsx` + `video-timeline-canvas.tsx` (нативная заглушка) | отрисовка и привязка к жестам | TK-629 |
| `src/video-monitor.web.tsx` + `video-monitor.tsx` | монитор | TK-630 |
| `src/video-bin.tsx`, `src/video-inspector.tsx`, `src/video-inspector-fields.ts` | бин, инспектор (таблица 13) | TK-631 |
| `src/video-editor.tsx`, `screens/video/sequences/[id]/index.tsx` | сборка экрана V-06 | TK-632 |
| `src/video-color-wheels-math.ts`, `video-color-curves-math.ts`, `video-scopes-math.ts`, `video-color-wheels.web.tsx`, `video-color-curves.web.tsx`, `video-scopes.web.tsx` | FN-E-15…17 | TK-633 |

## 17. Тесты

1. **Векторы** `vectors/editor.json`: каждая секция — отдельный блок тестов; дробные числа (`photo`, `env`, колёса, `schedule_envelope`) — допуск 1e-9, остальное — точное равенство (в том числе порядок массивов). Документы: `base` и варианты `docs.<имя>`; длительности источников — `source_duration_ticks`.
2. **Свойства (TS, генератор раздела 4.3 документа 16):** (П5) для 200 случайных команд из FN-T-15…27 с допустимыми аргументами — после каждой успешной команды `validateDocument` не выдаёт VT-03/VT-04/VT-07; (П6) `inverse` возвращает исходный документ канонически; (П7) `undo` ×N после N команд возвращает исходный документ (снимки).
3. **DOM-тесты** (vitest, jsdom): события указателя на canvas с заданными `getBoundingClientRect` дают ровно команды из `gestures` (`GS-03`, `GS-06`, `GS-10`, `GS-14`); клавиатура — команды из `shortcuts`; монитор с поддельными медиа-элементами (`play/pause/currentTime` как свойства, события вызываются тестом) исполняет действия `PB-02` (кроссфейд: два элемента видимы, у второго `opacity = 0.1` при `t = 3,6 с`) и `PB-04` (после 4-й ошибки — `Snackbar` с `PLAYBACK_SOURCE_FAILED`); пересинхронизация: элемент «отстаёт» на 200 мс → `currentTime` выставлен; на 100 мс → не трогается.
4. **Рендер (TK-634):** вариант `short` (запас головы 0,2 с) компилируется в `xfade` длительностью 0,4 с (`offset` по FN-R-04 от фактической длительности), план содержит `{clip: c2, requested: 1 с, effective: 0,4 с}`; фото с `ease_in_out` в середине клипа даёт масштаб 1,1 (как `CA-08`).
5. **e2e:** потоки F-41…F-43 ([19](19-user-flows-e2e.md)).

## 18. Требования

| ID | Требование |
|---|---|
| VID-ED-01 | Состав кадра, эффективный переход, движение фото и огибающие звука вычисляются FN-E-01…06 и совпадают с `editor.json`; рендер использует те же FN-E-02/03. |
| VID-ED-02 | Монитор воспроизводит монтаж без пауз на стыках при готовых источниках (предзагрузка ≥ 2 с), показывает кроссфейд, затемнение через чёрное, появление, фото с движением; при неготовности — ожидание, а не рассинхронизация. |
| VID-ED-03 | Звук предпросмотра микшируется в Web Audio по огибающим FN-E-05; скорость клипа сохраняет высоту тона; ограничения предпросмотра (без цвета, ducking, громкости; беззвучные неподдерживаемые кодеки) видны пользователю. |
| VID-ED-04 | Ссылки воспроизведения получаются по Bearer, живут ≤ 60 с, обновляются при перемотке и ошибке; после 3 неудач клип помечается, воспроизведение продолжается. |
| VID-ED-05 | Таймлайн: геометрия, хит-тест, масштаб, видимая область и привязка — по FN-E-07…10. |
| VID-ED-06 | Каждый жест таблицы раздела 9 порождает ровно указанную команду или ни одной; недопустимый перенос откатывается без ошибки. |
| VID-ED-07 | Горячие клавиши — таблица раздела 10; не срабатывают в полях ввода и при наборе IME. |
| VID-ED-08 | Команды FN-T-15…28 — с ошибками в указанном порядке, обратимы; заблокированная дорожка не меняется. |
| VID-ED-09 | Отмена/возврат — снимками, не менее 200 шагов или 32 МиБ. |
| VID-ED-10 | Инспектор — таблица раздела 13: каждое поле вызывает свою команду, одно изменение = один шаг отмены. |
| VID-ED-11 | Колёса, кривые и осциллограммы — FN-E-15…17. |
| VID-ED-12 | Нативные сборки показывают понятный отказ для поверхностей редактора (D-24). |

Критерии приёмки ACC-63…ACC-75 и трассировка — [17](17-acceptance-and-dod.md); карточки TK-620…634 — [15](15-task-cards.md), раздел 12.

## 19. Изменения других документов

[03](03-photographer-screens.md) (V-06…V-08 ссылаются на 23; зависимость `expo-video` только для V-03 на нативе); [04](04-data-model-and-api.md) (эндпоинт раздела 15); [07](07-timeline-edl-and-render-spec.md) (3.2: линейный звуковой кроссфейд; 3.3: правило сокращения — FN-E-02; 6.5: сглаживание — FN-E-03; 5: полный список команд); [10](10-testing-acceptance-rollout.md) (A-10 только для V-03; WP-19); [13](13-data-structures.md) (константы, DS-24: `VIDEO_PLAYBACK_UNSUPPORTED`, `limit_code` `PLAYBACK_LINK_RATE`, DS-31); [14](14-function-catalog.md) (модуль E, FN-T-15…28, FN-U-15…17, FN-G-15); [15](15-task-cards.md) (раздел 12, TK-606 — составная); [16](16-test-logic.md); [17](17-acceptance-and-dod.md); [19](19-user-flows-e2e.md) (F-13 исправлен, F-41…F-43, E2E-34…36); [18](18-use-cases.md) (UC-47…49); [21](21-review-report.md) (раздел 7); [README](README.md); `vectors/README.md`.
