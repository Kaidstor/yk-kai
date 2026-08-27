package command

import (
	"context"
	"fmt"
	"io"

	"github.com/Kaidstor/yk-kai/internal/config"
	"github.com/Kaidstor/yk-kai/internal/exit"
	"github.com/Kaidstor/yk-kai/internal/output"
)

type check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Details string `json:"details"`
}

// cmdDoctor отвечает на вопрос «почему не работает»: где настройки, какой
// токен подставился, кем видит нас YouTrack, доступен ли проект и жива ли доска.
func cmdDoctor(ctx context.Context, p *output.Printer, _ []string) int {
	c, cfg, token, err := client()
	if err != nil {
		return p.Fail("doctor", exit.Tool, "auth", "%s", err)
	}

	checks := []check{
		{Name: "config", OK: true, Details: configLabel(cfg)},
		{Name: "token", OK: true, Details: fmt.Sprintf("%s, %s", token.Source, config.Mask(token.Value))},
	}
	code := exit.OK

	me, err := c.Me(ctx)
	if err != nil {
		checks = append(checks, check{Name: "user", Details: err.Error()})
		code = exit.Tool
	} else {
		checks = append(checks, check{Name: "user", OK: true, Details: me.Login})
	}

	if exists, err := c.ProjectExists(ctx, cfg.Project); err != nil {
		// Список проектов лежит под админским эндпоинтом, и токену его могут
		// не дать — для работы с задачами это не помеха.
		checks = append(checks, check{Name: "project", Details: "не проверить: " + err.Error()})
	} else if exists {
		checks = append(checks, check{Name: "project", OK: true, Details: cfg.Project})
	} else {
		checks = append(checks, check{Name: "project", Details: cfg.Project + " не виден токену"})
		code = exit.Tool
	}

	switch {
	case !cfg.BoardConfigured():
		checks = append(checks, check{Name: "board", Details: "не настроена (agile_id + sprint_id)"})
	default:
		if name, err := c.SprintExists(ctx); err != nil {
			checks = append(checks, check{Name: "board", Details: err.Error()})
			code = exit.Tool
		} else {
			checks = append(checks, check{Name: "board", OK: true, Details: name})
		}
	}

	return p.Result("doctor", code, map[string]any{"checks": checks, "host": cfg.Host},
		func(w io.Writer) {
			for _, ch := range checks {
				mark := "fail"
				if ch.OK {
					mark = "ok  "
				}
				fmt.Fprintf(w, "%s  %-8s %s\n", mark, ch.Name, ch.Details)
			}
		})
}

func configLabel(cfg config.Config) string {
	if cfg.Path == "" {
		return cfg.Host + " (только из окружения)"
	}
	return cfg.Host + ", " + cfg.Path
}
