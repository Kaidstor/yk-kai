package command

import (
	"context"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Kaidstor/yk-kai/internal/exit"
	"github.com/Kaidstor/yk-kai/internal/output"
)

// orInParens ловит free-text через or в скобках: YouTrack такой запрос не
// разбирает и молча отдаёт пустой список — выглядит как «ничего не нашлось».
var orInParens = regexp.MustCompile(`\([^)]*\bor\b[^)]*\)`)

func cmdSearch(ctx context.Context, p *output.Printer, args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 20, "сколько задач вернуть")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return p.Fail("search", exit.Tool, "usage", "%s", err)
	}

	query := strings.TrimSpace(strings.Join(positional, " "))
	if query == "" {
		return p.Fail("search", exit.Tool, "usage",
			"нужен запрос: yk-kai search \"project: PROJ #Unresolved for: <login>\"")
	}

	c, _, _, err := client()
	if err != nil {
		return p.Fail("search", exit.Tool, "auth", "%s", err)
	}

	// Гоча: `for: me` под этим токеном отдаёт почти пустоту — YouTrack
	// считает «мной» не владельца токена. Подставляем логин явно.
	if strings.Contains(strings.ToLower(query), "for: me") {
		me, err := c.Me(ctx)
		if err != nil {
			return fail(p, "search", err)
		}
		query = replaceFold(query, "for: me", "for: "+me.Login)
		p.Warn("`for: me` заменено на `for: %s`: под токеном me отдаёт не то", me.Login)
	}

	if orInParens.MatchString(query) {
		p.Warn("free-text через `or` в скобках YouTrack не разбирает — вернётся пустой список; разбей на отдельные запросы")
	}

	issues, err := c.Search(ctx, query, *limit)
	if err != nil {
		return fail(p, "search", err)
	}

	return p.Result("search", exit.OK, map[string]any{
		"query": query,
		"count": len(issues),
		"items": issues,
	}, func(w io.Writer) {
		if len(issues) == 0 {
			fmt.Fprintln(w, "ничего не нашлось")
			return
		}
		for _, issue := range issues {
			fmt.Fprintf(w, "%-10s %-16s %s\n", issue.IDReadable, issue.Field("State"), issue.Summary)
		}
		fmt.Fprintf(w, "\nвсего: %d\n", len(issues))
	})
}

func replaceFold(s, old, new string) string {
	idx := strings.Index(strings.ToLower(s), strings.ToLower(old))
	if idx < 0 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}
