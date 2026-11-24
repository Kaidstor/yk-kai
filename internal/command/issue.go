package command

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Kaidstor/yk-kai/internal/exit"
	"github.com/Kaidstor/yk-kai/internal/output"
	"github.com/Kaidstor/yk-kai/internal/youtrack"
)

// wish — поле, которое просили выставить, и его ожидаемое значение.
type wish struct {
	name   string
	want   string
	update youtrack.FieldUpdate
}

type fieldFlags struct {
	typ      string
	priority string
	state    string
	assignee string
	summary  string
	desc     string
	descFile string
}

func (f *fieldFlags) bind(fs *flag.FlagSet, withSummary bool) {
	fs.StringVar(&f.typ, "type", "", "Type")
	fs.StringVar(&f.priority, "priority", "", "Priority")
	fs.StringVar(&f.state, "state", "", "State")
	fs.StringVar(&f.assignee, "assignee", "", "Assignee (логин)")
	fs.StringVar(&f.desc, "desc", "", "описание")
	fs.StringVar(&f.descFile, "desc-file", "", "описание из файла")
	if withSummary {
		fs.StringVar(&f.summary, "summary", "", "заголовок")
	}
}

// wishes переводит флаги в обновления полей, попутно проверяя значения.
func (f *fieldFlags) wishes() ([]wish, error) {
	var out []wish

	if f.typ != "" {
		v, err := youtrack.Normalize("Type", f.typ, youtrack.Types)
		if err != nil {
			return nil, err
		}
		out = append(out, wish{name: "Type", want: v, update: youtrack.EnumField("Type", v)})
	}
	if f.priority != "" {
		v, err := youtrack.Normalize("Priority", f.priority, youtrack.Priorities)
		if err != nil {
			return nil, err
		}
		out = append(out, wish{name: "Priority", want: v, update: youtrack.EnumField("Priority", v)})
	}
	if f.state != "" {
		v, err := youtrack.Normalize("State", f.state, youtrack.States)
		if err != nil {
			return nil, err
		}
		out = append(out, wish{name: "State", want: v, update: youtrack.StateField(v)})
	}
	if f.assignee != "" {
		out = append(out, wish{name: "Assignee", want: f.assignee, update: youtrack.UserField(f.assignee)})
	}

	return out, nil
}

func cmdCreate(ctx context.Context, p *output.Printer, args []string) int {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f fieldFlags
	f.bind(fs, false)
	noBoard := fs.Bool("no-board", false, "не цеплять к доске")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return p.Fail("create", exit.Tool, "usage", "%s", err)
	}

	summary := strings.TrimSpace(strings.Join(positional, " "))
	if summary == "" {
		return p.Fail("create", exit.Tool, "usage", "нужен заголовок: yk-kai create \"Summary\" [--type Bug]")
	}

	c, cfg, _, err := client()
	if err != nil {
		return p.Fail("create", exit.Tool, "auth", "%s", err)
	}

	// Умолчания: задача без типа, приоритета и исполнителя теряется в трекере.
	if f.typ == "" {
		f.typ = "Task"
	}
	if f.priority == "" {
		f.priority = "Normal"
	}
	if f.assignee == "" {
		f.assignee = cfg.Assignee
	}

	wishes, err := f.wishes()
	if err != nil {
		return p.Fail("create", exit.Tool, "usage", "%s", err)
	}

	description, err := readText(f.desc, f.descFile)
	if err != nil {
		return p.Fail("create", exit.Tool, "usage", "не прочитать описание: %s", err)
	}

	updates := make([]youtrack.FieldUpdate, 0, len(wishes))
	for _, w := range wishes {
		updates = append(updates, w.update)
	}

	created, err := c.Create(ctx, youtrack.CreateRequest{
		Project:     cfg.Project,
		Summary:     summary,
		Description: description,
		Fields:      updates,
	})
	if err != nil {
		return fail(p, "create", err)
	}

	issue, err := c.Get(ctx, created.IDReadable, false)
	if err != nil {
		return fail(p, "create", err)
	}
	issue, missing, err := ensureFields(ctx, c, issue, wishes)
	if err != nil {
		return fail(p, "create", err)
	}

	code := exit.OK
	onBoard := false
	switch {
	case *noBoard:
		p.Warn("на доску не цепляли (--no-board)")
	case !cfg.BoardConfigured():
		p.Warn("доска не настроена (agile_id + sprint_id) — задача создана без привязки")
	default:
		if err := c.Board(ctx, created.ID); err != nil {
			p.Warn("на доску не прицепилось (%s) — проверь руками: %s", err, cfg.IssueURL(created.IDReadable))
			code = exit.NotApplied
		} else {
			onBoard = true
		}
	}

	if len(missing) > 0 {
		p.Warn("YouTrack не принял поля: %s", strings.Join(missing, ", "))
		code = exit.NotApplied
	}

	// Гоча: `idReadable` дублирует `id` намеренно. У `get` данные — это сырая
	// задача, где `id` внутренний (`2-1782`), а `idReadable` человеческий
	// (`PROJ-123`); в собранных здесь картах `id` человеческий. Читающий
	// `data.idReadable` получает одно и то же в любой команде — без него
	// агент видит `null` и решает, что задача не создалась (так завели дубль).
	data := map[string]any{
		"id":         created.IDReadable,
		"idReadable": created.IDReadable,
		"url":        cfg.IssueURL(created.IDReadable),
		"summary":    summary,
		"fields":     fieldMap(issue),
		"board":      onBoard,
		"missing":    missing,
	}

	return p.Result("create", code, data, func(w io.Writer) {
		fmt.Fprintf(w, "%s  %s\n", created.IDReadable, cfg.IssueURL(created.IDReadable))
		printFields(w, issue)
		if onBoard {
			fmt.Fprintln(w, "  на доске: да")
		}
	})
}

func cmdGet(ctx context.Context, p *output.Printer, args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	full := fs.Bool("full", false, "описание, связи и комментарии")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return p.Fail("get", exit.Tool, "usage", "%s", err)
	}

	if len(rest) == 0 {
		return p.Fail("get", exit.Tool, "usage", "нужен id задачи: yk-kai get PROJ-123 [--full]")
	}
	id := rest[0]
	if len(rest) > 1 && rest[1] == "full" {
		*full = true
	}

	c, _, _, err := client()
	if err != nil {
		return p.Fail("get", exit.Tool, "auth", "%s", err)
	}

	issue, err := c.Get(ctx, id, *full)
	if err != nil {
		return fail(p, "get", err)
	}

	return p.Result("get", exit.OK, issue, func(w io.Writer) {
		fmt.Fprintf(w, "%s — %s\n", issue.IDReadable, issue.Summary)
		printFields(w, issue)
		if !*full {
			return
		}
		if issue.Description != "" {
			fmt.Fprintf(w, "\n== описание ==\n%s\n", issue.Description)
		}
		printLinks(w, issue)
		for _, comment := range issue.Comments {
			fmt.Fprintf(w, "\n== комментарий %s, %s ==\n%s\n",
				comment.Author.Login, comment.CreatedAt().Format("2006-01-02 15:04"), comment.Text)
		}
	})
}

func cmdSet(ctx context.Context, p *output.Printer, args []string) int {
	return setIssue(ctx, p, "set", args)
}

func cmdState(ctx context.Context, p *output.Printer, args []string) int {
	if len(args) < 2 {
		return p.Fail("state", exit.Tool, "usage", "нужно: yk-kai state PROJ-123 \"In Progress\"")
	}
	id := args[0]
	state := strings.Join(args[1:], " ")
	return setIssue(ctx, p, "state", []string{id, "--state", state})
}

func setIssue(ctx context.Context, p *output.Printer, command string, args []string) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f fieldFlags
	f.bind(fs, true)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return p.Fail(command, exit.Tool, "usage", "%s", err)
	}
	if len(positional) == 0 {
		return p.Fail(command, exit.Tool, "usage",
			"нужен id задачи: yk-kai set PROJ-123 --state Review --assignee <login>")
	}
	id := positional[0]

	wishes, wishErr := f.wishes()
	if wishErr != nil {
		return p.Fail(command, exit.Tool, "usage", "%s", wishErr)
	}

	var summary, description *string
	if f.summary != "" {
		summary = &f.summary
	}
	if f.desc != "" || f.descFile != "" {
		text, err := readText(f.desc, f.descFile)
		if err != nil {
			return p.Fail(command, exit.Tool, "usage", "не прочитать описание: %s", err)
		}
		description = &text
	}

	if len(wishes) == 0 && summary == nil && description == nil {
		return p.Fail(command, exit.Tool, "usage", "нечего менять: задай --type/--priority/--state/--assignee/--summary/--desc")
	}

	c, cfg, _, err := client()
	if err != nil {
		return p.Fail(command, exit.Tool, "auth", "%s", err)
	}

	updates := make([]youtrack.FieldUpdate, 0, len(wishes))
	for _, w := range wishes {
		updates = append(updates, w.update)
	}
	issue, err := c.Update(ctx, id, updates, summary, description)
	if err != nil {
		return fail(p, command, err)
	}

	issue, missing, err := ensureFields(ctx, c, issue, wishes)
	if err != nil {
		return fail(p, command, err)
	}

	code := exit.OK
	if len(missing) > 0 {
		p.Warn("YouTrack не принял поля: %s", strings.Join(missing, ", "))
		code = exit.NotApplied
	}

	changes := setChanges(issue, wishes, summary != nil, description != nil)
	data := map[string]any{
		"id":           issue.IDReadable,
		"idReadable":   issue.IDReadable,
		"summary":      issue.Summary,
		"url":          cfg.IssueURL(issue.IDReadable),
		"customFields": issue.Fields,
		"fields":       fieldMap(issue),
		"changed":      changes,
		"missing":      missing,
	}
	return p.Result(command, code, data, func(w io.Writer) {
		fmt.Fprintln(w, changeLine(issue, changes))
	})
}

// change — что set просил поменять и что YouTrack вернул после записи.
type change struct {
	Field   string `json:"field"`
	Want    string `json:"want,omitempty"`
	Value   string `json:"value"`
	Applied bool   `json:"applied"`
}

func setChanges(issue *youtrack.Issue, wishes []wish, summary, description bool) []change {
	out := make([]change, 0, len(wishes)+2)
	if summary {
		out = append(out, change{Field: "summary", Value: issue.Summary, Applied: true})
	}
	if description {
		out = append(out, change{Field: "description", Value: "обновлено", Applied: true})
	}
	for _, w := range wishes {
		got := issue.Field(w.name)
		out = append(out, change{Field: w.name, Want: w.want, Value: got, Applied: strings.EqualFold(got, w.want)})
	}
	return out
}

func changeLine(issue *youtrack.Issue, changes []change) string {
	parts := make([]string, 0, len(changes))
	for _, ch := range changes {
		value := ch.Value
		if value == "" {
			value = "—"
		}
		if ch.Applied {
			parts = append(parts, fmt.Sprintf("%s → %s", ch.Field, value))
		} else {
			parts = append(parts, fmt.Sprintf("%s не применилось (сейчас %s, просили %s)", ch.Field, value, ch.Want))
		}
	}
	return issue.IDReadable + ": " + strings.Join(parts, ", ")
}

func cmdComment(ctx context.Context, p *output.Printer, args []string) int {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "текст из файла")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return p.Fail("comment", exit.Tool, "usage", "%s", err)
	}
	if len(positional) == 0 {
		return p.Fail("comment", exit.Tool, "usage", "нужен id задачи: yk-kai comment PROJ-123 \"текст\"")
	}

	id := positional[0]
	text, err := readText(strings.Join(positional[1:], " "), *file)
	if err != nil {
		return p.Fail("comment", exit.Tool, "usage", "не прочитать текст: %s", err)
	}
	if strings.TrimSpace(text) == "" {
		return p.Fail("comment", exit.Tool, "usage",
			"пустой комментарий: передай текстом, --file или на stdin")
	}

	c, cfg, _, err := client()
	if err != nil {
		return p.Fail("comment", exit.Tool, "auth", "%s", err)
	}
	if err := c.Comment(ctx, id, text); err != nil {
		return fail(p, "comment", err)
	}

	data := map[string]any{"id": id, "idReadable": id, "url": cfg.IssueURL(id), "chars": len(text)}
	return p.Result("comment", exit.OK, data, func(w io.Writer) {
		fmt.Fprintf(w, "комментарий добавлен: %s\n", cfg.IssueURL(id))
	})
}

// ensureFields сверяет прочитанную задачу с желаемым и дожимает поля, которые
// не применились.
//
// Гоча ради этой функции: YouTrack отвечает на POST успехом и тогда, когда
// поле не записал. Так задачи заводились без Assignee и с дефолтным
// приоритетом, а вызывающий видел «создано» и шёл дальше.
func ensureFields(ctx context.Context, c *youtrack.Client, issue *youtrack.Issue, wishes []wish) (*youtrack.Issue, []string, error) {
	retry := diff(issue, wishes)
	if len(retry) == 0 {
		return issue, nil, nil
	}

	updates := make([]youtrack.FieldUpdate, 0, len(retry))
	for _, w := range retry {
		updates = append(updates, w.update)
	}
	issue, err := c.Update(ctx, issue.IDReadable, updates, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	var missing []string
	for _, w := range diff(issue, wishes) {
		missing = append(missing, fmt.Sprintf("%s=%s", w.name, w.want))
	}
	return issue, missing, nil
}

func diff(issue *youtrack.Issue, wishes []wish) []wish {
	var out []wish
	for _, w := range wishes {
		if !strings.EqualFold(issue.Field(w.name), w.want) {
			out = append(out, w)
		}
	}
	return out
}

func fieldMap(issue *youtrack.Issue) map[string]string {
	out := map[string]string{}
	for _, f := range issue.Fields {
		if v := f.String(); v != "" {
			out[f.Name] = v
		}
	}
	return out
}

func printFields(w io.Writer, issue *youtrack.Issue) {
	fields := fieldMap(issue)
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %s: %s\n", name, fields[name])
	}
}

func printLinks(w io.Writer, issue *youtrack.Issue) {
	for _, link := range issue.Links {
		if len(link.Issues) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n== %s ==\n", link.Side())
		for _, linked := range link.Issues {
			fmt.Fprintf(w, "  %-10s %-16s %s\n", linked.IDReadable, linked.Field("State"), linked.Summary)
		}
	}
}
