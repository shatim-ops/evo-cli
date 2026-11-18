### Безопасность

- **Предупреждения о подсети и группе безопасности называют их имена в кавычках.** `deploy_plan`, `provision`,
  `evo deploy` и `evo provision`: «subnet … has no DNS servers», «security group … has no ingress rule for TCP 22»
  и «… does not allow the VM's system egress» вставляли имя из облака как есть — перевод строки в нём начинал
  новую строку текста evo. Стало: имя в кавычках с экранированием. `reason` и коды не меняются.
