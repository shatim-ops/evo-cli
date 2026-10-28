### Добавлено

- **`evo vm flavors` и MCP `list_flavors` показывают долю vCPU, GPU, spot и зоны flavor** (план 1.5, С5; аудит «слепые зоны» 1.5, FB15-1, FB15-3).
  - Новые поля: `cpu_share` — гарантированная доля vCPU, как её пишет каталог (`1:10` — 10 % ядра); `gpu`; `spot` — прерываемая ВМ; `zones` — зоны, где ВМ с этим flavor можно создать.
  - Поле выводится, только если каталог его прислал.
  - Добавлены фильтры `cpu`, `ram`, `share`, `zone`, `gpu`, `spot` (флаги `evo vm flavors --share 1:1 --zone ru.AZ-1 …`). Фильтр оставляет только flavor, о которых каталог говорит то, что спрошено.
  - Превью `evo vm create`, `evo deploy` и MCP `provision`, `deploy`, `deploy_plan` называет долю vCPU, GPU и spot выбранного flavor: «lowcost10-2-4 (2 vCPU, доля vCPU 1:10, 4 ГБ RAM)». Раньше `lowcost10-2-4` выглядел так же, как flavor 1:1 того же размера.
  - Автоподбор flavor не меняется: его правило — 1.6.
