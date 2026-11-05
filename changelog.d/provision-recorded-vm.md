### Исправлено

- **Отказ `provision` с именем ВМ, записанной в state этой папки, говорит правду.** Было: «this folder's evo did not create it (its state does not record it)», хотя state её записывает. Стало: «this folder's evo created it, and provision makes a new VM», подсказка — другое имя, `vm_get vm_id=<id>`, а удалять ли — спросить человека. Код выхода, `reason` (`USAGE`) и `existing_id` прежние. Нашёл агентный прогон 1.7.0.
