// Package command — CLI-слой: роутер, разбор флагов и сами команды.
package command

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Kaidstor/yk-kai/internal/config"
	"github.com/Kaidstor/yk-kai/internal/exit"
	"github.com/Kaidstor/yk-kai/internal/output"
	"github.com/Kaidstor/yk-kai/internal/youtrack"
)

// version подставляется ldflags при сборке; в go run остаётся dev.
var version = "dev"

// commandTimeout ограничивает команду целиком: внутри клиент ретраит запросы,
// и без общего срока залипший YouTrack держал бы вызов бесконечно.
const commandTimeout = 3 * time.Minute

const usage = `yk-kai — задачи YouTrack из терминала и из агента

Использование:
  yk-kai <команда> [аргументы]

Задачи:
  create <summary>              создать задачу и прицепить к доске
  get <ISSUE> [--full]          показать задачу; --full — описание, связи, комментарии
  set <ISSUE> [флаги]           сменить поля задачи
  state <ISSUE> <состояние>     сокращение для set --state
  comment <ISSUE> [текст]       комментарий; без текста читается stdin
  board <ISSUE>                 прицепить существующую задачу к доске
  search <запрос> [--limit N]   поиск задач

Связи и время:
  link <ISSUE> <тип> <ISSUE>    depends on, subtask of, parent for, relates to, duplicates
  links <ISSUE>                 показать связи задачи
  worklog <ISSUE> <длит> [текст]  списать время: 2h, 45m, "1d 2h"

Служебное:
  config [init]                 показать настройки или создать шаблон
  doctor                        токен, пользователь, проект, доска
  version

Флаги create:
  --type <Type>                 Bug | Task | Feature | Exception | Cosmetics |
                                Usability Problem | Performance Problem | Epic (по умолчанию Task)
  --priority <Priority>         Show-stopper | Critical | Major | Normal | Minor (по умолчанию Normal)
  --assignee <login>            по умолчанию assignee из настроек
  --state <State>               Backlog | To Do | In Progress | Review | Testing & Deploy | Done
  --desc <текст>                описание; без флага читается stdin
  --desc-file <файл>            описание из файла — так и надо для markdown
  --no-board                    не цеплять к доске

Флаги set: --type, --priority, --state, --assignee, --summary, --desc, --desc-file

Значения полей понимаются и по-русски («Ошибка», «Обычная»): в интерфейсе они
показаны так, а API принимает только английские.

Настройки — файл ` + "`config.json`" + ` (путь: yk-kai config), поверх него переменные
YOUTRACK_HOST, YOUTRACK_PROJECT, YOUTRACK_ASSIGNEE, YOUTRACK_AGILE_ID,
YOUTRACK_SPRINT_ID, YOUTRACK_TOKEN_REF. Токен — $YOUTRACK_TOKEN или sec по
token_ref.

Общие флаги:
  --human                       вывод для человека вместо JSON
  --json                        машиночитаемый вывод (по умолчанию)
  -h, --help                    справка

Коды выхода:
  0 сделано и проверено         2 ошибка инструмента или аргументов   4 YouTrack не ответил
  1 YouTrack не применил        3 не найдено`

// Run разбирает argv и возвращает код выхода. Ошибки печатает сам — наверх
// уходит только число.
func Run(args []string) int {
	human, args := splitGlobalFlags(args)

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Println(usage)
		return exit.OK
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	p := output.New(human)
	name, rest := args[0], args[1:]

	switch name {
	case "create":
		return cmdCreate(ctx, p, rest)
	case "get":
		return cmdGet(ctx, p, rest)
	case "set":
		return cmdSet(ctx, p, rest)
	case "state":
		return cmdState(ctx, p, rest)
	case "comment":
		return cmdComment(ctx, p, rest)
	case "board":
		return cmdBoard(ctx, p, rest)
	case "search":
		return cmdSearch(ctx, p, rest)
	case "link":
		return cmdLink(ctx, p, rest)
	case "links":
		return cmdLinks(ctx, p, rest)
	case "worklog":
		return cmdWorklog(ctx, p, rest)
	case "config":
		return cmdConfig(p, rest)
	case "doctor":
		return cmdDoctor(ctx, p, rest)
	case "version", "--version":
		return p.Result("version", exit.OK,
			map[string]string{"version": version},
			func(w io.Writer) { fmt.Fprintln(w, version) })
	default:
		return p.Fail("", exit.Tool, "usage",
			"неизвестная команда %q; yk-kai --help покажет список", name)
	}
}

// splitGlobalFlags вынимает общие флаги из любого места argv: подкоманды
// разбирают остаток своим FlagSet, а он падает на неизвестном флаге.
func splitGlobalFlags(args []string) (human bool, rest []string) {
	for _, a := range args {
		switch a {
		case "--human":
			human = true
		case "--json":
			human = false
		default:
			rest = append(rest, a)
		}
	}
	return human, rest
}

// parseArgs разбирает флаги, встречающиеся и после позиционных аргументов, и
// возвращает позиционные в порядке следования.
//
// Гоча: стандартный flag.Parse останавливается на первом не-флаге, поэтому
// `yk-kai search "project: PROJ" --limit 5` уезжал в YouTrack запросом
// «project: PROJ --limit 5» и получал invalid_query — выглядит как сломанный
// синтаксис запроса, а сломан разбор argv.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// client отдаёт готовый клиент, настройки и источник токена: doctor его
// печатает, а остальные команды используют в тексте ошибки авторизации.
func client() (*youtrack.Client, config.Config, config.Token, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cfg, config.Token{}, err
	}
	token, err := cfg.LoadToken()
	if err != nil {
		return nil, cfg, config.Token{}, err
	}
	return youtrack.New(cfg, token.Value), cfg, token, nil
}

// fail переводит ошибку YouTrack в код выхода: агент решает по коду, а
// подробности читает из конверта.
func fail(p *output.Printer, command string, err error) int {
	kind := youtrack.KindOf(err)
	code := exit.Tool
	switch kind {
	case youtrack.KindNotFound:
		code = exit.NotFound
	case youtrack.KindTimeout:
		code = exit.Timeout
	}
	return p.Fail(command, code, kind, "%s", err.Error())
}

// readText берёт текст из аргумента, файла или stdin.
//
// Гоча ради этой функции: кавычка внутри текста ломает передачу текста
// аргументом через шелл. Файл и stdin такого не умеют.
func readText(inline, file string) (string, error) {
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(raw), "\n"), nil
	}
	if inline != "" {
		return inline, nil
	}
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", nil
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return "", nil // терминал, никто ничего не передавал
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(raw), "\n"), nil
}
