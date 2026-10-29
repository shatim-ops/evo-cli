### Исправлено

- **Tab у `evo ca deploy --image` больше не предлагает образы ВМ** (аудит Fable 1.5, FB150-1). Раньше дополнение брало `image_list` (`Ubuntu-24.04` и т. п.), а `ca_deploy` принимает образ контейнера из Artifact Registry. Теперь у этого аргумента дополнения нет. У `evo vm create --image` и `evo deploy --image` оно прежнее.
