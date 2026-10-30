### Изменено

- **Человеческий вывод называет оставшиеся ссылки «имя (id)»: подсеть превью `evo deploy apply` (из плана `deploy_plan`), интерфейс `evo vm fip-attach` и целей `evo lb tg-create`, зону по умолчанию `evo k8s options`.** Ответ несёт новые поля: `interface_name` у `fip_attach` и в превью `tg_create` (имя интерфейса прочитано вместе с ВМ), `defaults.zone_name` у `k8s_options`, `network.subnet_name` в плане `deploy_plan` (план, сохранённый до 1.6, его не несёт — превью покажет id). Значения прежних полей не меняются; в тело запроса `tg_create` имя не уходит.
