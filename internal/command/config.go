package command

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Kaidstor/yk-kai/internal/config"
	"github.com/Kaidstor/yk-kai/internal/exit"
	"github.com/Kaidstor/yk-kai/internal/output"
)

// cmdConfig показывает настройки или создаёт шаблон. Никаких значений
// инстанса в бинаре нет, поэтому первый вопрос нового пользователя — «куда
// это писать», и отвечать на него должен сам CLI.
func cmdConfig(p *output.Printer, args []string) int {
	path := config.Path()

	if len(args) > 0 && args[0] == "init" {
		if _, err := os.Stat(path); err == nil {
			return p.Fail("config", exit.Tool, "usage", "%s уже существует", path)
		}
		if err := config.Save(config.Template(), path); err != nil {
			return p.Fail("config", exit.Tool, "io", "не записать %s: %s", path, err)
		}
		return p.Result("config", exit.OK, map[string]any{"path": path, "created": true},
			func(w io.Writer) {
				fmt.Fprintf(w, "шаблон записан: %s\n", path)
				fmt.Fprintln(w, "заполни host, project и token_ref (ссылка на секрет в sec)")
			})
	}

	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		return p.Fail("config", exit.Tool, "usage", "%s", err)
	}

	code := exit.OK
	if errors.Is(err, config.ErrNoConfig) {
		p.Warn("настройки не заполнены: yk-kai config init")
		code = exit.NotApplied
	}

	return p.Result("config", code, map[string]any{
		"path":      path,
		"host":      cfg.Host,
		"project":   cfg.Project,
		"assignee":  cfg.Assignee,
		"agile_id":  cfg.AgileID,
		"sprint_id": cfg.SprintID,
		"token_ref": cfg.TokenRef,
		"board":     cfg.BoardConfigured(),
	}, func(w io.Writer) {
		fmt.Fprintf(w, "файл:      %s\n", path)
		fmt.Fprintf(w, "host:      %s\n", cfg.Host)
		fmt.Fprintf(w, "project:   %s\n", cfg.Project)
		fmt.Fprintf(w, "assignee:  %s\n", cfg.Assignee)
		fmt.Fprintf(w, "доска:     %s\n", boardLabel(cfg))
		fmt.Fprintf(w, "token_ref: %s\n", cfg.TokenRef)
	})
}

func boardLabel(cfg config.Config) string {
	if !cfg.BoardConfigured() {
		return "не настроена (agile_id + sprint_id)"
	}
	return cfg.AgileID + " / " + cfg.SprintID
}
