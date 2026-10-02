# 14. Матрица покрытия ТЗ 4.1

Каждый раздел требований привязан к проекту, пакету и проверке. Наличие строки означает запланированное покрытие, не статус выполнения. Полный scope не ограничивается базовым цветовым переносом.

| Раздел ТЗ | Документы implementation | Пакеты | Проверки |
|---|---|---|---|
| 1 | 01 06 08 10 | P18 | N08 |
| 2 | 01 11 | P00 P16 | DB01 |
| 3 | 02 03 | P02 P03 | DB02 |
| 4 | 02 04 09 | P04 P11 | IMM01 IMM02 |
| 5 | 03 09 | P04 P06 | SHARE01 SHARE02 |
| 6 | 10 | P08 | UI02 UI03 |
| 7 | 06 09 | P06 | UP03 |
| 8 | 09 | P07 | DL01 DL02 DL03 |
| 9 | 04 09 | P04 | BULK01 |
| 10 | 03 04 | P03 | AUTH01 AUTH02 |
| 11 | 10 | P15 | UI03 |
| 12 | 10 | P15 | UI01 |
| 13 | 02 04 | P02 P10 | DB01 DB03 STYLE02 |
| 14 | 09 | P06 P16 | UP02 GC01 |
| 15 | 05 | P05 | fault matrix |
| 16 | 06 15 | P00 P05 | contracts-check |
| 17 | 05 | P05 | fault matrix |
| 18 | 05 | P10 P11 | IMM03 STYLE01 |
| 19 | 06 07 | P10 | N01 STYLE01 |
| 20 | 06 08 | P11 P14 | N08 |
| 21 | 07 11 | P01 P09 | N01 |
| 22 | 08 11 | P13 | N07 |
| 23 | 06 08 11 | P12 | N08 |
| 24 | 08 11 | P14 | N08 |
| 25 | 11 | P01 | doctor |
| 26 | 07 | P09 | N05 |
| 27 | 07 | P10 | STYLE01 N01 |
| 28 | 07 | P10 | N01 N02 |
| 29 | 07 | P10 | N03 N06 |
| 30 | 07 09 | P11 | N04 N05 |
| 31 | 08 | P13 | N07 |
| 32 | 08 | P13 | N07 |
| 33 | 08 | P12 | N08 |
| 34 | 08 10 | P12 P15 | N08 UI01 |
| 35 | 08 | P14 | N08 |
| 36 | 08 09 | P14 | N08 DL01 |
| 37 | 07 15 | P09 | STYLE02 |
| 38 | 08 15 | P13 | N07 |
| 39 | 06 15 | P14 | contracts-check |
| 40 | 06 15 | P11 | IMM03 |
| 41 | 11 | P16 | SHARE02 UI03 |
| 42 | 11 12 | P16 P18 | P01 performance |
| 43 | 11 | P16 | RESTORE01 |
| 44 | 12 | P18 | N01 N02 N03 N04 N05 N06 |
| 45 | 12 | P18 | N07 N08 |
| 46 | 12 | P18 | UI01 UI02 UI03 IMM01 IMM02 IMM03 |
| 47 | 13 | P18 | release gates |
| 48 | 01 11 | P01 | source/license register |

Подразделы 4.1/13.1/46.1: неизменяемые версии, parent lineage, new collection transaction, v1/v2 profile history, subsets и idempotency — 01 ADR A02/A03/A16/A17,02,04,05,09; P04/P10/P11; IMM01–03,STYLE01–02. Подраздел12.1: парный мастер и именованные профили —10,P15.

Дополнительные полнообъёмные функции: explicit paired import, search by example via tenant-local DINO cosine, contact sheets, sensor dust, glasses/hair/cloth fixes, clothing masks, product background, virtual staging/twilight, window pull/HDR, outpaint, x2/x4, RAW, PNG alpha, metadata/ICC, per-person overrides, feedback pairs, guest upload moderation, multi-volume ZIP, native export —06/08/09/10. Они не считаются «опциональными к реализации».

## Автоматическая проверка покрытия

`contracts-check` требует: каждый registry op имеет input/output/params schema+adapter+unit fixture; каждый endpoint имеет OpenAPI request/response/error/RBAC test; каждый tenant table FORCE RLS и composite FK либо явное историческое исключение; каждый пакет ссылается на существующий документ; каждый пункт ТЗ имеет functional test mapping. TODO в реализованной mandatory ветке блокирует release. Численные/визуальные evidence отдельно от функционального PASS.
