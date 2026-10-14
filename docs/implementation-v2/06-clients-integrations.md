# Клиенты, доставка, агент и совместимость

## Профессиональный редактор

Существующие createEditorState/applyEditorCommand/undoEditor/redoEditor/beginEditorPreview/acceptsEditorPreview остаются основой. Не заменять их новым несвязанным store.

EditorCommand = {id,base_revision,source_sha,type:set_param|mask_stroke|set_reference|accept_candidate,payload,created_at}. Persisted draft CAS; history локально максимум 200 entries, старые compact в checkpoint. Undo generation выбирает предыдущий reference, не запускает модель повторно.

Добавить функции:

- loadLoupe(version,rect,scale,revision): запрос region derivative максимум1024², server checks source and project; cache key source/recipe/rect/scale/ICC. Никогда скачать master для loupe. Новый POST /assets/{id}/region-previews {version_id,rect,recipe,max_edge}→ticket, GET через existing run outputs. Tile zoom обозначает rendered scale, proxy не выдаётся за100%.
- applyBatchCommand(selection,command): frozen version IDs, preview affected count, по каждому source-bound override; conflicts показать отдельно.
- compareFrames(ids[2..6],viewport): synchronized pan/zoom, одно canonical color-managed preview space, keyboard select/reject; не менять исходники.
- buildSceneBoard(analysis): groups, reference, confidence, outliers; reference change создаёт новый analysis, не мутирует old plan.
- recordApprovedCorrection(before,after,context): после успешного server save, event_id persisted до retry; learn по явному opt-in.
- showIdentityReport(report): overlays changed/generated/protected, pass/review/not measurable, никаких утверждений «личность доказана» по одной метрике.

Маршруты: /sessions/[id], /sessions/[id]/scenes, /learning/[id], /capture/[id], /semantic/[id], /deliveries/[id], /interchange/[id]. Все строки RU/BG/EN, aria labels и keyboard shortcuts через общую таблицу commands. Locale меняется без сброса черновика. Offline: read cached proxies и draft edits; при reconnect CAS conflict предлагает сравнение, silent overwrite запрещён.

## Layers и masks

Точные LayerStack/Layer/MaskExpr определены в contracts/schemas.json. MaskExpr recursive union ref|union|intersect|subtract|invert; глубина≤16, nodes≤256, один Canvas. Eval: union=max(a,b), intersect=min(a,b), subtract=max(0,a-b), invert=1-a; continuous probability semantics фиксируются версией. Blend в рабочем linear RGB после явной конверсии; opacity применяется один раз; geometry layers только prefix stack, иначе VALIDATION_FAILED. Plan compiler топологически вычисляет reuse masks, сохраняет outputs per layer; imported unsupported blend mode→unsupported в mapping report.

## Studio agent

apps/studio-agent использует локальную SQLite journal (device_id,sequence,relative_path,sha,state,retries). В состояния discovered→stable→hashed→uploading→verified→receipt→done; error/retry сохраняются после restart. WatchFolder.scan: whitelist directory, no symlink escape, file size/mtime unchanged два опроса по1s, rehash перед complete. Sequence монотонна, сохраняется до network IO. Existing multipart upload adapter, bounded parallel parts=2, max queued bytes configurable; никаких удалений camera originals.

Camera adapters имеют interface list_devices/connect/capture_events/read_file/disconnect, explicit tested camera matrix. Watch-folder — гарантированный v2 baseline; native tethering отдельный пакет с реальным hardware acceptance. «Поддержка всех камер» не заявляется. Credentials OS keychain, TLS pinned operator CA, token scope device/project. Revocation останавливает completion. QA alerts push через existing SSE, polling fallback5s.

## Delivery

DeliveryPolicy {required_presets:UUID[1..8], optional_presets:UUID[0..8], require_review:bool=true, metadata_policy, provenance:off|required, signing_policy_id?:UUID, signing_policy_sha?:SHA256, allow_omitted:bool=false}. При `off` оба signing-поля отсутствуют; при `required` оба обязательны и фиксируют ровно одну текущую квалифицированную V31 SigningPolicy до создания child-запусков. Ротация/отзыв этой политики блокирует admission, не выбирает новую автоматически. Пока путь V31 required не квалифицирован сквозным образом, валидный `required` возвращает 503. Freeze preset config/hash при создании. buildDeliverables(snapshot,policy) создаёт existing export/download jobs; contact sheet только как явно выбранный preset/resource, полная slot matrix15. Required item fail блокирует package ready. Optional omission требует явного reason. publishPackage проверяет весь manifest в одной transaction; signed links только через existing intents/redeem. Auto-send email только после явно включённого user workflow и сохранённого списка адресатов; default publication без отправки.

## Interchange

BundleManifest/InterchangePlan/ParsedInterchange/MappingReport заданы schemas.json; фазы parse→canonical ingest→resolve/review и точный interchange subset описаны15. Entries≤5000. Archive extraction total bytes quota, path traversal/symlinks/zip bomb reject. External paths никогда не открываются напрямую; imports содержат attached files или explicit user mapping.

parseXMP(bytes)->ExternalEdits: XML external entities/DTD/network disabled; bounded1MiB; whitelist ratings/labels/crop/orientation и documented namespaces. mapExternalEdits(edits,development_profile)->MappingReport: rating exact; normalized crop пересчитать через orientation; WB/curves только при известной совместимости decoder/color profile; иначе unsupported, не приблизительно без сообщения.

Capture One proprietary catalogs не редактировать SQL напрямую. v2 baseline — XMP subset + rendered TIFF и mask files; PSD/PSB требует отдельной format qualification, не заявляется baseline; полноценный round-trip только после qualification официально поддерживаемого формата. exportBundle(snapshot)->Artifact: manifest, sidecars, masks, rendered derivatives; atomic archive publish. importBundle→draft коллекция, никакой перезаписи existing originals.

## C2PA

buildManifest(lineage,operations,reviews,privacy)->Manifest: source hashes, operation/version/model lock и generated claims; не включать emails/person embeddings/GPS. signManifest/export работает в изолированном signer с key handle, не raw key в params. verifySignedExport повторно читает finished bytes; signature valid не означает правдивость всей истории, root capture authenticity не заявляется без trusted source credentials. Key rotation создаёт новую signing policy, old signed outputs immutable.
