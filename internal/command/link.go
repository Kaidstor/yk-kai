package command

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Kaidstor/yk-kai/internal/exit"
	"github.com/Kaidstor/yk-kai/internal/output"
	"github.com/Kaidstor/yk-kai/internal/youtrack"
)

// LinkTypes — команды связывания, которые понимает YouTrack.
//
// Связи ставятся через commands API, а не через /api/issues/.../links: там
// нужен id типа связи и правильная сторона (s — исходящая, t — входящая), и
// ошибиться стороной проще, чем попасть.
var LinkTypes = []string{
	"depends on",
	"is required for",
	"relates to",
	"subtask of",
	"parent for",
	"duplicates",
}

// linkArgs разбирает `<ISSUE> <тип из нескольких слов> <ISSUE>`.
func linkArgs(command string, args []string) (source, linkType, target string, err error) {
	if len(args) < 3 {
		return "", "", "", fmt.Errorf("нужно: yk-kai %s PROJ-123 \"depends on\" PROJ-456; типы: %s",
			command, strings.Join(LinkTypes, ", "))
	}
	source = args[0]
	target = args[len(args)-1]
	linkType, err = youtrack.Normalize("тип связи", strings.Join(args[1:len(args)-1], " "), LinkTypes)
	return source, linkType, target, err
}

func cmdLink(ctx context.Context, p *output.Printer, args []string) int {
	source, normalized, target, err := linkArgs("link", args)
	if err != nil {
		return p.Fail("link", exit.Tool, "usage", "%s", err)
	}

	c, cfg, _, err := client()
	if err != nil {
		return p.Fail("link", exit.Tool, "auth", "%s", err)
	}

	if err := c.Command(ctx, normalized+" "+target, []string{source}); err != nil {
		return fail(p, "link", err)
	}

	// Пустой ответ commands API — это успех, но не доказательство: сверяем.
	issue, err := c.Links(ctx, source)
	if err != nil {
		return fail(p, "link", err)
	}

	code := exit.OK
	if !hasLink(issue, target) {
		p.Warn("связь не появилась в задаче — проверь глазами: %s", cfg.IssueURL(source))
		code = exit.NotApplied
	}

	data := map[string]any{
		"source": source,
		"type":   normalized,
		"target": target,
		"links":  issue.Links,
	}
	return p.Result("link", code, data, func(w io.Writer) {
		fmt.Fprintf(w, "%s %s %s\n", source, normalized, target)
		printLinks(w, issue)
	})
}

// cmdUnlink снимает связь командой `remove <тип> <ISSUE>`.
//
// Гоча проверки: названия сторон в ответе не совпадают с командами (relates to
// приходит как «Relates»), поэтому снятие сверяется числом связей с целевой
// задачей до и после, а не поиском связи нужного типа.
func cmdUnlink(ctx context.Context, p *output.Printer, args []string) int {
	source, normalized, target, err := linkArgs("unlink", args)
	if err != nil {
		return p.Fail("unlink", exit.Tool, "usage", "%s", err)
	}

	c, cfg, _, err := client()
	if err != nil {
		return p.Fail("unlink", exit.Tool, "auth", "%s", err)
	}

	before, err := c.Links(ctx, source)
	if err != nil {
		return fail(p, "unlink", err)
	}
	if countLinks(before, target) == 0 {
		return p.Fail("unlink", exit.NotFound, "not_found",
			"у %s нет связей с %s", before.IDReadable, target)
	}

	if err := c.Command(ctx, "remove "+normalized+" "+target, []string{source}); err != nil {
		return fail(p, "unlink", err)
	}

	issue, err := c.Links(ctx, source)
	if err != nil {
		return fail(p, "unlink", err)
	}

	code := exit.OK
	if countLinks(issue, target) >= countLinks(before, target) {
		p.Warn("связь %s %s не снялась — проверь тип связи (yk-kai links %s) или глазами: %s",
			normalized, target, source, cfg.IssueURL(source))
		code = exit.NotApplied
	}

	data := map[string]any{
		"source":     source,
		"idReadable": issue.IDReadable,
		"type":       normalized,
		"target":     target,
		"links":      issue.Links,
	}
	return p.Result("unlink", code, data, func(w io.Writer) {
		fmt.Fprintf(w, "%s: снята связь %s %s\n", source, normalized, target)
		if hasAny(issue) {
			printLinks(w, issue)
		}
	})
}

func cmdLinks(ctx context.Context, p *output.Printer, args []string) int {
	if len(args) == 0 {
		return p.Fail("links", exit.Tool, "usage", "нужен id задачи: yk-kai links PROJ-123")
	}

	c, _, _, err := client()
	if err != nil {
		return p.Fail("links", exit.Tool, "auth", "%s", err)
	}

	issue, err := c.Links(ctx, args[0])
	if err != nil {
		return fail(p, "links", err)
	}

	return p.Result("links", exit.OK, issue, func(w io.Writer) {
		if len(issue.Links) == 0 || !hasAny(issue) {
			fmt.Fprintf(w, "%s: связей нет\n", issue.IDReadable)
			return
		}
		fmt.Fprintf(w, "%s\n", issue.IDReadable)
		printLinks(w, issue)
	})
}

func hasLink(issue *youtrack.Issue, target string) bool {
	return countLinks(issue, target) > 0
}

func countLinks(issue *youtrack.Issue, target string) int {
	n := 0
	for _, link := range issue.Links {
		for _, linked := range link.Issues {
			if strings.EqualFold(linked.IDReadable, target) {
				n++
			}
		}
	}
	return n
}

func hasAny(issue *youtrack.Issue) bool {
	for _, link := range issue.Links {
		if len(link.Issues) > 0 {
			return true
		}
	}
	return false
}
