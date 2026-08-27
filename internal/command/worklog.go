package command

import (
	"context"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/Kaidstor/yk-kai/internal/exit"
	"github.com/Kaidstor/yk-kai/internal/output"
)

// duration — запись длительности YouTrack: 45m, 2h, "1d 2h 30m", 1w.
var duration = regexp.MustCompile(`^(\d+[wdhm]\s*)+$`)

func cmdWorklog(ctx context.Context, p *output.Printer, args []string) int {
	fs := flag.NewFlagSet("worklog", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	date := fs.String("date", "", "дата списания YYYY-MM-DD (по умолчанию сегодня)")
	file := fs.String("file", "", "текст из файла")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return p.Fail("worklog", exit.Tool, "usage", "%s", err)
	}

	if len(rest) < 2 {
		return p.Fail("worklog", exit.Tool, "usage",
			"нужно: yk-kai worklog PROJ-123 2h \"что делал\"")
	}

	id, spent := rest[0], strings.TrimSpace(rest[1])
	if !duration.MatchString(spent) {
		return p.Fail("worklog", exit.Tool, "usage",
			"непонятная длительность %q: ожидается запись YouTrack — 45m, 2h, \"1d 2h\"", spent)
	}

	text, err := readText(strings.Join(rest[2:], " "), *file)
	if err != nil {
		return p.Fail("worklog", exit.Tool, "usage", "не прочитать текст: %s", err)
	}

	day := time.Now()
	if *date != "" {
		day, err = time.ParseInLocation("2006-01-02", *date, time.Local)
		if err != nil {
			return p.Fail("worklog", exit.Tool, "usage", "дата в формате YYYY-MM-DD: %s", err)
		}
	}
	when := workDate(day)

	c, _, _, err := client()
	if err != nil {
		return p.Fail("worklog", exit.Tool, "auth", "%s", err)
	}
	if err := c.WorkItem(ctx, id, spent, text, when); err != nil {
		return fail(p, "worklog", err)
	}

	data := map[string]any{
		"id":       id,
		"duration": spent,
		"date":     when.Format("2006-01-02"),
		"text":     text,
	}
	return p.Result("worklog", exit.OK, data, func(w io.Writer) {
		fmt.Fprintf(w, "%s: списано %s за %s\n", id, spent, when.Format("2006-01-02"))
	})
}

// workDate переводит календарный день в метку, которую YouTrack не сдвинет.
//
// Гоча: время списания он округляет до дня по UTC, поэтому полночь по местному
// времени восточнее Гринвича уезжает на сутки назад — запрошенное 26-е
// записывалось 25-м. Полдень UTC переживает сдвиг в обе стороны.
func workDate(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC)
}
