# План кодогенерации и приёмка

## Пакеты и зависимости

Каждый пакет: contracts → миграция → repository → service → worker/compiler → API → UI → integration/evidence. Готовность файла или компиляция не закрывает пакет.

| ID | Depends | Изменения / основные файлы | Приёмка |
|---|---|---|---|
| V20 | — | актуализировать Pxx по трассировке; model bundle/doctor/capacity; tests model qualification | каждый existing operation имеет ready/unqualified/unsupported и evidence; legacy hashes сохранены; own registry/output policy, GC/purge integration and denied-role tests |
| V21 | V20 | workflow foundation, versioned documents, draft persistence and approval, GC and RLS | cross-tenant, fence, cancel-before-commit, quota/replay, GC race; own registry/output policy, GC/purge integration and denied-role tests |
| V22 | V21 | sessionbrain + session operations  | 5000 frames bounded memory; deterministic groups; manual refs; unknown time; no skin homogenization; own registry/output policy, GC/purge integration and denied-role tests |
| V23 | V22 | learning, CorrectionEvent/fit/evaluate/safety review/promote + identity.compare | leakage-free sessions; small data reject; explicit activation; rollback; revoke consent; own registry/output policy, GC/purge integration and denied-role tests |
| V24 | V23 | capture , studio-agent, QA | crash/restart dedup, rotated credentials, queue lag, real camera/watch-folder fixture; own registry/output policy, GC/purge integration and denied-role tests |
| V25 | V24 | RAW/lens/metadata/export qualification | supported camera matrix, ICC RGB16, alpha, HDR limits, ZIP64 >4GiB real IO; own registry/output policy, GC/purge integration and denied-role tests |
| V26 | V25 | editor loupe/layers/scene board/keyboard | real browser+native, stale response, offline CAS, RU/BG/EN, 5000 gallery; own registry/output policy, GC/purge integration and denied-role tests |
| V27 | V26 | semanticedit , transfer/review | parallax/occlusion fallback, per-target decisions, no extra target mutation; own registry/output policy, GC/purge integration and denied-role tests |
| V28 | V27 | portrait.open_eyes/glare/hair/clothing/reflection/relight | real model and donor corpus, protected unchanged, human approval, unavailable state; own registry/output policy, GC/purge integration and denied-role tests |
| V29 | V28 | panorama/focus_stack/negative/perspective | multi-source lineage, moving objects,200MP cap, color/geometry QA; own registry/output policy, GC/purge integration and denied-role tests |
| V30 | V29 | delivery+interchange  | full required bundle atomic, revoked share, XMP hostile input, unsupported fields visible; own registry/output policy, GC/purge integration and denied-role tests |
| V31 | V30 | provenance signer  | valid/invalid signature, key rotation, privacy redaction, actual SDK export verification; own registry/output policy, GC/purge integration and denied-role tests |
| V32 | V31 | lifecycle reconciliation, deployment, capacity and restore acceptance; no deferred registry registration | mixed workloads on3060, restore, role matrix, actual end-to-end acceptance; own registry/output policy, GC/purge integration and denied-role tests |

00196–00207 apply in numeric order; packages may develop on code branches but feature activation waits migration predecessors. Не применять куски одной migration вручную. Contracts generation should extend existing generator and schemas, not fork it.

## Правило принятия работы модели

Перед пакетом прочитать README/01–20 и current code, проверить migration head. После изменения: contract drift, package unit, real DB migration fresh+upgrade, targeted integration. Для frontend — tests+typecheck+browser flow. Для image code — synthetic invariants и real-photo qualification. Не повторять все suites после каждого комментария; полный regression при merge-ready package.

Work report JSON {package_id,status:planned|implementing|awaiting_evidence|accepted,source_commit,dirty_tree_hash,contracts_hash,checks:[{command,exit_code,evidence_sha}],requirements:[{id,status,evidence}],limitations:[]}. Evidence привязан к dirty working tree hash, одного HEAD недостаточно. accepted запрещён при missing required check.

## Сквозные сценарии

A01: 500 RAW→ingest→group→manual reference→Apply→review→export→client download; проверить исходные hashes и source lineage.
A02: два tenant с одинаковыми UUID в input запроса: no access/no artifact disclosure, direct DB real roles отрицательные.
A03: crash после PUT до DB adoption, повтор Rabbit delivery, expired fence: один canonical result, quota accounting ровно один раз.
A04: кадр изменён после планирования: старый snapshot неизменен; новые правки требуют нового плана.
A05: consent revoked во время fit: epoch check запрещает promotion и новые identity matches.
A06: group photograph с маленькими/скрытыми лицами: unknown не превращается в closed-eye/pass.
A07: semantic removal переносится на occluded scene: unsupported, original preserved.
A08: purge tenant с каждым типом v2 artifact: object cleanup→metadata allowlist; foreign refs/leases не висят; privilege test запрещает DELETE вне списка.
A09: C2PA подпись после watermark/encode; изменение одного byte нарушает validation; подпись не создаётся до финального encode.
A10: retry delivery failure не публикует частичный required bundle.
A11: client disconnect во время multipart: resume без дубля receipt/asset.
A12: old Apply v4 run завершается при v2 rollout; compiler version не меняется задним числом.

## Фото-корпус и критерии

Использовать минимум existing N01–N08 из ../implementation/12-verification.md; добавить session-consistency, donor-eye, glare, reflection, film и panorama subsets. Для qualification разделять calibration/test по людям и съёмкам; персональное runtime learning разделяет по сессиям, same person допускается по10. Никакого training на blind test. Хранить consent/license, input SHA, camera/light metadata и human decisions.

Quality acceptance: сохранение protected pixels до следующих геометрических/цветовых операций; exact bypass strength0; no NaN; bounded clipping; отчёт доли rejected/unsupported. Задача Session Brain: уменьшить median внутри-сценового EV residual ≥20% против baseline без роста clipping>0.5pp; human score не хуже baseline. Learning ≥10% уменьшения weighted correction error на holdout, safety nonregression. Это проектные пороги, их прохождение измеряется.

ML quality в product: показывать только доступные измеренные capabilities. Если reflection/eye adapter не прошёл корпус, выпуск core разрешён с явно обозначенным отсутствием этой функции, но цель полного покрытия дорожной карты остаётся незакрытой.

## Производительность и выпуск

Профили 24MP и45MP, cold/warm, параллельно5 viewers+ingest+preview+Apply+ZIP. Измерять p50/p95 queue wait отдельно от compute. On-set p95≤5s после proxy target; interactive proxy p95≤1s CPU effect target; тяжёлая генерация async, без выдуманного SLA. No OOM/leak при8h test; проверить cancel latency и fairness. Restore на изолированном узле с hash verification всех sample assets и восстановлением auth/reviews/queues.

Релиз включает immutable image/model/ICC locks, SBOM, supported cameras/formats matrix, measured capacity, migration backup/runbook и documented recovery. Один RTX3060 узел — пилот студии; число платящих пользователей не выводится из VRAM без workload benchmark.

## Прослеживаемость предложений

Session Brain→V22; learning→V23; on-set QA→V24; identity primitive→V23, portrait integration→V28; semantic batch→V27; C2PA→V31; appliance→V20/V24/V32; delivery→V30; interchange→V30; RAW/UI parity→V25/V26; additional portrait effects→V28; panorama/focus/negative→V29. Каждый пункт должен иметь accepted evidence; «123 endpoints есть» не является критерием полноты.
