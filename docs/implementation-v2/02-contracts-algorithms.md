# Контракты и алгоритмы

## Общие типы

Реиспользовать ArtifactRef {artifact_id: UUID, sha256: lowercase hex64}, Canvas, MaskRef, WorkingImage, TransformChain. Все новые JSON contracts: schema_version integer literal 1, extra fields forbidden, finite numbers, JCS hashing. UUID передаются строками. Revision bigint передаётся decimal string. Время RFC3339 UTC; отсутствующее время — null. Размер JSON body ≤1 MiB; большие массивы как artifacts. Snapshot ≤5000 items, страницы API 1..100.



## Session Brain



FaceObservation содержит face_id/observation_id, box, 68 inline landmarks и embedding512|null; точная schema и адаптер16. Person IDs локальны проекту; matching только после opt-in, никогда межтенантный поиск.


Функции workers/photo_worker/operations/session/:

- build_descriptors(inputs, calibration, check_active)->Iterator[FrameDescriptor]: читать proxy ≤2048px, извлекать признаки одним locked model, писать shard по 128 кадров. Некачественное лицо не включать в identity matching.
- group_scenes(descriptors, policy)->ScenePlan: stable sort по corrected capture time затем ordinal; неизвестные timestamps отдельно. Для каждого кадра top-k по cosine в bounded blocks; запрещать edges через time gap, сравнивать color/light distance с thresholds из calibration. Agglomerative merge разрешать только при удовлетворении cluster diameter, чтобы цепочка похожих кадров не объединила разные сцены. Tie-break ordinal. Group UUIDv5(snapshot_id, policy_sha+":"+sorted ordinals joined comma), exact10.
- choose_reference(group, manual_reference)->ordinal: ручной reference должен принадлежать группе; иначе максимум quality минус clipping penalty, tie-break меньший ordinal.
- solve_consistency(group, reference, protected)->CorrectionPlan: детерминированное сопоставление яркости и neutral RGB по формулам 10-algorithms-calibration.md; reference фиксирует gauge. EV ±1, gains [0.8,1.25], защищать ручные overrides. Не выравнивать разные лица к единому тону кожи. При отсутствующем neutral RGB выполнять только допустимый EV, WB0 с reason; при invalid luminance no_change.
- validate_scene_plan(plan,snapshot)->Validation: cardinality, source hashes, finite/bounds, references, group uniqueness. Любая ошибка блокирует Apply.

## Обучение на коррекциях



- freeze_examples(events, consent_epoch)->TrainingManifest: исключить отменённые/неподтверждённые/несовместимые версии; split по целым session IDs SHA256(session_id, seed), не по кадрам. Сессии не пересекают train/holdout. Для person profile один человек намеренно присутствует в обеих частях; оценивается перенос между его сессиями. Перенос на неизвестных людей этим тестом не доказывается.
- fit_delta_model(train,base)->CandidateProfile: frozen scene features → ridge regression residuals к разрешённым параметрам; стандартизация train-only; lambda выбирается из [0.01,0.1,1,10] cross-validation по sessions. Никакого fine-tuning diffusion в v2.
- evaluate_candidate(candidate,holdout,baseline)->Evaluation: MAE параметров + rendered color error + clipping/identity guards; сравнить с baseline на тех же кадрах. Promotion требует неухудшения safety и ≥10% уменьшения weighted error. Меньше 2 holdout sessions — INSUFFICIENT_DATA; rendered input closure и safety review по15.
- promote_profile(expected_revision,candidate,evaluation)->ProfileVersion: только явное подтверждение владельца профиля, CAS активной версии. Rollback создаёт новый activation event, не удаляет историю.

## On-set QA



- assess_capture(frame,faces,policy)->QAReport: резкость на нормализованных eye/face ROI, open-eye classifier только для достаточно крупных видимых глаз, clipping отдельно от subject mask; glare только с квалифицированным detector. Нельзя маркировать unavailable как clean.
- update_shot_coverage(shot_requirements,confirmed_persons,frame)->Coverage: учитывать только подтверждённые person IDs; автоматический match создаёт suggestion. Нельзя объявлять человека отсутствующим, если detection confidence недостаточен.
- coalesce_alerts(previous,report)->AlertDelta: ключ session+check+person, подавлять одинаковое предупреждение 10 s; critical нового кадра сохранять в журнале. Цель p95 ≤5 s после доступного proxy, не после нажатия затвора.

## Semantic batch и identity



- transfer_intent(reference,target,intent)->TransferCandidate: feature correspondences + RANSAC homography для плоского фона; при parallax/occlusion baseline возвращает unsupported; альтернативный adapter включается только новой qualified operation version с собственным контрактом. Нет достоверного соответствия →unsupported. Не переносить face pixels по общей homography.
- validate_transfer(candidate,target)->Decision: mask domain/canvas, correspondence inlier ratio и protected overlap по calibration. Всегда preview; destructive content removal требует review каждой цели или явно выбранной группы с показом всех thumbnails.
- compile_transfer(approved_plan)->OverrideRefs: pinned targets; один immutable override на target; source mismatch →conflict, никогда автоматический rebase.
- compare_identity(before,after,mask,calibration)->IdentityReport: face matching, normalized embedding similarity, landmark drift, texture diagnostics и exact outside-mask pixel check до downstream export. Метрики не доказывают сохранение личности сами по себе. Нет пригодного лица →not_measurable с review, а не pass.


Точные структуры: [schemas.json](contracts/schemas.json). Нормативные формулы: [10](10-algorithms-calibration.md).
