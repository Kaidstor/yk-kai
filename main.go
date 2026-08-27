// yk-kai — работа с YouTrack из терминала и из агента: задачи, поля,
// комментарии, доска, связи, списание времени.
//
// Точка входа тонкая: вся логика — в пакетах internal/. Карта:
//
//	internal/command   CLI-слой: роутер Run, usage, разбор флагов, все команды
//	internal/youtrack  клиент REST API: ретраи, разбор отказов, поля задачи
//	internal/config    хост, проект, доска, дефолтный исполнитель, токен
//	internal/output    единый JSON-конверт и человекочитаемый режим
//	internal/exit      коды выхода как контракт с вызывающим
package main

import (
	"os"

	"github.com/Kaidstor/yk-kai/internal/command"
)

func main() { os.Exit(command.Run(os.Args[1:])) }
