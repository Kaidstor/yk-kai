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

func cmdLink(ctx context.Context, p *output.Printer, args []string) int {
	if len(args) < 3 {
		return p.Fail("link", exit.Tool, "usage",
			"нужно: yk-kai link PROJ-123 \"depends on\" PROJ-456; типы: %s", strings.Join(LinkTypes, ", "))
	}

	source := args[0]
	target := args[len(args)-1]
	linkType := strings.Join(args[1:len(args)-1], " ")

	normalized, err := youtrack.Normalize("тип связи", linkType, LinkTypes)
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
	for _, link := range issue.Links {
		for _, linked := range link.Issues {
			if strings.EqualFold(linked.IDReadable, target) {
				return true
			}
		}
	}
	return false
}

func hasAny(issue *youtrack.Issue) bool {
	for _, link := range issue.Links {
		if len(link.Issues) > 0 {
			return true
		}
	}
	return false
}
