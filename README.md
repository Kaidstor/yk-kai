# yk-kai

CLI для YouTrack: задачи, поля, комментарии, доска, связи, списание времени.
Работает и руками, и из агента — по умолчанию печатает JSON-конверт, по
`--human` человекочитаемый текст.

Адрес инстанса, проект и доска в коде не зашиты: всё берётся из файла настроек.

## Установка

```sh
go install github.com/Kaidstor/yk-kai@latest
```

Бинарь окажется в `~/go/bin/yk-kai`.

## Настройка

```sh
yk-kai config init     # создаст шаблон и покажет путь
yk-kai config          # текущие значения
yk-kai doctor          # токен, пользователь, проект, доска
```

Файл — `~/.config/yk-kai/config.json` (переопределяется `$YK_KAI_CONFIG`):

```json
{
  "host": "https://example.youtrack.cloud",
  "project": "PROJ",
  "assignee": "alice",
  "agile_id": "",
  "sprint_id": "",
  "token_ref": "work/YOUTRACK_API_KEY"
}
```

- `assignee` — на кого вешать задачи по умолчанию;
- `agile_id` и `sprint_id` — доска и спринт: созданная через API задача на доску
  не попадает сама, с этими полями `create` кладёт её туда. Пустые — привязка
  отключена;
- `token_ref` — ссылка на секрет в [sec](https://github.com/Kaidstor/sec), вида
  `<проект>/<KEY>`.

Любое поле переопределяется окружением: `YOUTRACK_HOST`, `YOUTRACK_PROJECT`,
`YOUTRACK_ASSIGNEE`, `YOUTRACK_AGILE_ID`, `YOUTRACK_SPRINT_ID`,
`YOUTRACK_TOKEN_REF`. Токен ищется в `$YOUTRACK_TOKEN` / `$YOUTRACK_API_KEY`, а
затем в sec по `token_ref` — в argv он не попадает никогда.

## Команды

```sh
yk-kai create "Summary" --type Bug --priority Major     # + прицепит к доске
cat desc.md | yk-kai create "Summary" --type Bug        # описание со stdin
yk-kai get PROJ-123 --full                              # + описание, связи, комментарии
yk-kai set PROJ-123 --state Review --assignee alice
yk-kai state PROJ-123 Done
yk-kai comment PROJ-123 --file msg.md
yk-kai board PROJ-123                                   # прицепить к доске отдельно
yk-kai search "project: PROJ #Unresolved for: alice" --limit 20
yk-kai link PROJ-123 "depends on" PROJ-456
yk-kai links PROJ-123
yk-kai worklog PROJ-123 2h "что делал"
```

Значения полей понимаются и по-русски («Ошибка», «Обычная», «В работе»): в
интерфейсе YouTrack они показаны так, а API принимает только английские.

## Скилл для агента

В `skills/yk/` лежит скилл для Claude Code: команды, коды выхода, синтаксис
поиска и грабли API, на которых инструмент уже обжигался. Поставить себе:

```sh
cp -r skills/yk ~/.claude/skills/yk
```

Дальше агент зовёт `yk-kai` вместо самодельных запросов к API — и понимает, что
означает код выхода 1.

## Коды выхода

| код | смысл |
|---|---|
| 0 | сделано и проверено |
| 1 | YouTrack принял запрос, но не применил (поле, доска, связь) |
| 2 | ошибка инструмента или аргументов |
| 3 | не найдено |
| 4 | YouTrack не ответил вовремя |

## Что делает за тебя

- **Цепляет задачу к доске.** Созданная через API задача на доске не появляется
  сама — `create` кладёт её в спринт из настроек, `board` чинит забытое.
- **Проверяет, что поля применились.** YouTrack отвечает успехом и на запрос,
  который ничего не записал; `create` и `set` перечитывают задачу, дожимают
  непринятые поля и возвращают код 1, если поле так и не встало.
- **Валидирует значения до запроса** и печатает список допустимых.
- **Разбирает флаги после позиционных аргументов** — `search "..." --limit 5`
  работает, а не уезжает в текст запроса.
- **Отличает отказ от результата**: YouTrack умеет вернуть `{"error": ...}` с
  HTTP 200, и без проверки это выглядит как успех.
- **Кодирует пробелы в запросе как `%20`** — с плюсом YouTrack отвечает
  `invalid_query` на корректный запрос.
