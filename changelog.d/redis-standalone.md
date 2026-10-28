### Исправлено

- **`evo redis create --type STANDALONE` и MCP `redis_create` с `type=STANDALONE` создают кластер** (G140-6). Было: evo слал в API `replicas: 0`, и платформа отвечала 400 «replicas must be positive» — кластер STANDALONE через evo не создавался. Стало: в запросе — `replicas: 1`, один узел в шарде, как его принимает API. Аргумент `replicas` не меняет смысла: реплики без мастера, для STANDALONE — 0; превью — один узел в шарде.
