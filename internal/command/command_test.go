package command

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kaidstor/yk-kai/internal/youtrack"
)

func TestSplitGlobalFlags(t *testing.T) {
	human, rest := splitGlobalFlags([]string{"get", "PROJ-1", "--human"})
	if !human {
		t.Fatal("--human не распознан в конце argv")
	}
	if strings.Join(rest, " ") != "get PROJ-1" {
		t.Fatalf("остаток argv: %v", rest)
	}

	human, _ = splitGlobalFlags([]string{"--json", "get", "PROJ-1"})
	if human {
		t.Fatal("--json должен выключать человеческий режим")
	}
}

func TestFieldFlagsWishes(t *testing.T) {
	f := fieldFlags{typ: "ошибка", priority: "Major", state: "in progress", assignee: "alice"}
	wishes, err := f.wishes()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if len(wishes) != 4 {
		t.Fatalf("ожидали 4 поля, получили %d", len(wishes))
	}

	got := map[string]string{}
	for _, w := range wishes {
		got[w.name] = w.want
	}
	want := map[string]string{"Type": "Bug", "Priority": "Major", "State": "In Progress", "Assignee": "alice"}
	for name, value := range want {
		if got[name] != value {
			t.Fatalf("%s: получили %q, ожидали %q", name, got[name], value)
		}
	}
}

func TestFieldFlagsRejectsUnknownValue(t *testing.T) {
	f := fieldFlags{priority: "Blocker"}
	if _, err := f.wishes(); err == nil {
		t.Fatal("недопустимое значение приоритета должно отвергаться до запроса к API")
	}
}

func TestDiffFindsUnappliedFields(t *testing.T) {
	// Ровно тот случай, ради которого команды перечитывают задачу: YouTrack
	// ответил успехом, а Assignee не проставился.
	issue := &youtrack.Issue{Fields: []youtrack.CustomField{
		{Name: "Type", Value: json.RawMessage(`{"name":"Bug"}`)},
		{Name: "Priority", Value: json.RawMessage(`{"name":"Normal"}`)},
		{Name: "Assignee", Value: json.RawMessage(`null`)},
	}}
	wishes := []wish{
		{name: "Type", want: "Bug"},
		{name: "Priority", want: "Major"},
		{name: "Assignee", want: "alice"},
	}

	missing := diff(issue, wishes)
	if len(missing) != 2 {
		t.Fatalf("ожидали 2 непринятых поля, получили %d: %+v", len(missing), missing)
	}
	names := []string{missing[0].name, missing[1].name}
	if names[0] != "Priority" || names[1] != "Assignee" {
		t.Fatalf("не те поля: %v", names)
	}
}

func TestSetChangesReportValueAfterWrite(t *testing.T) {
	issue := &youtrack.Issue{IDReadable: "PROJ-1", Summary: "новый заголовок", Fields: []youtrack.CustomField{
		{Name: "State", Value: json.RawMessage(`{"name":"Done"}`)},
		{Name: "Assignee", Value: json.RawMessage(`null`)},
	}}
	wishes := []wish{
		{name: "State", want: "Done"},
		{name: "Assignee", want: "alice"},
	}

	changes := setChanges(issue, wishes, true, false)
	if len(changes) != 3 || changes[0].Field != "summary" || !changes[1].Applied || changes[2].Applied {
		t.Fatalf("изменения собраны не так: %+v", changes)
	}

	got := changeLine(issue, changes)
	want := "PROJ-1: summary → новый заголовок, State → Done, Assignee не применилось (сейчас —, просили alice)"
	if got != want {
		t.Fatalf("строка для человека:\n%s\nждали:\n%s", got, want)
	}
}

func TestReadText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desc.md")
	if err := os.WriteFile(path, []byte("## Заголовок\n\nтекст с 'кавычкой'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readText("", path)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !strings.Contains(got, "кавычкой") || strings.HasSuffix(got, "\n") {
		t.Fatalf("текст из файла разобран не так: %q", got)
	}

	// Файл важнее аргумента: если передали оба, берём файл.
	got, err = readText("аргумент", path)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if strings.Contains(got, "аргумент") {
		t.Fatalf("аргумент перебил --file: %q", got)
	}
}

func TestOrInParensDetected(t *testing.T) {
	if !orInParens.MatchString("project: PROJ #Unresolved (пресет* or сесси*)") {
		t.Fatal("free-text через or в скобках должен ловиться")
	}
	if orInParens.MatchString("project: PROJ #Unresolved for: alice") {
		t.Fatal("обычный запрос не должен считаться проблемным")
	}
}

func TestReplaceFold(t *testing.T) {
	got := replaceFold("project: PROJ #Unresolved For: Me", "for: me", "for: alice")
	if got != "project: PROJ #Unresolved for: alice" {
		t.Fatalf("получили %q", got)
	}
}

func TestResolveCommandAliases(t *testing.T) {
	cases := []struct {
		in   []string
		name string
		rest string
	}{
		{[]string{"show", "PROJ-1"}, "get", "PROJ-1"},
		{[]string{"view", "PROJ-1", "--full"}, "get", "PROJ-1 --full"},
		{[]string{"update", "PROJ-1", "--state", "Done"}, "set", "PROJ-1 --state Done"},
		{[]string{"edit", "PROJ-1", "--summary", "x"}, "set", "PROJ-1 --summary x"},
		{[]string{"list", "--query", "project: PROJ"}, "search", "--query project: PROJ"},
		{[]string{"ls", "project: PROJ"}, "search", "project: PROJ"},
		{[]string{"issue", "get", "PROJ-1"}, "get", "PROJ-1"},
		{[]string{"issues", "get", "PROJ-1"}, "get", "PROJ-1"},
		{[]string{"issue", "PROJ-1", "--full"}, "get", "PROJ-1 --full"},
		{[]string{"issue", "update", "PROJ-1", "--state", "Review"}, "set", "PROJ-1 --state Review"},
		{[]string{"issue", "create", "Fix bug"}, "create", "Fix bug"},
		{[]string{"issue", "--help"}, "help", ""},
		{[]string{"update", "--help"}, "help", ""},
		{[]string{"issue"}, "help", ""},
	}
	for _, tc := range cases {
		gotName, gotRest := resolveCommand(tc.in)
		if gotName != tc.name || strings.Join(gotRest, " ") != tc.rest {
			t.Fatalf("resolveCommand(%v) = (%q, %v), ждали (%q, %q)", tc.in, gotName, gotRest, tc.name, tc.rest)
		}
	}
}

func TestParseArgsFlagsAfterPositional(t *testing.T) {
	// Гоча flag.Parse: разбор останавливается на первом не-флаге, и `--limit 5`
	// уезжал внутрь поискового запроса.
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 20, "")

	positional, err := parseArgs(fs, []string{"project: PROJ #Unresolved", "--limit", "5"})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if *limit != 5 {
		t.Fatalf("--limit после позиционного не разобрался: %d", *limit)
	}
	if len(positional) != 1 || positional[0] != "project: PROJ #Unresolved" {
		t.Fatalf("позиционные аргументы: %v", positional)
	}
}

func TestParseArgsKeepsOrder(t *testing.T) {
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	state := fs.String("state", "", "")

	positional, err := parseArgs(fs, []string{"PROJ-1", "--state", "Review", "extra"})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if *state != "Review" {
		t.Fatalf("--state не разобрался: %q", *state)
	}
	if strings.Join(positional, "|") != "PROJ-1|extra" {
		t.Fatalf("порядок позиционных нарушен: %v", positional)
	}
}

func TestLinkArgsMultiWordType(t *testing.T) {
	source, linkType, target, err := linkArgs("unlink", []string{"PROJ-1", "relates", "to", "PROJ-2"})
	if err != nil {
		t.Fatal(err)
	}
	if source != "PROJ-1" || linkType != "relates to" || target != "PROJ-2" {
		t.Fatalf("разобрано не так: %q %q %q", source, linkType, target)
	}

	if _, _, _, err := linkArgs("unlink", []string{"PROJ-1", "PROJ-2"}); err == nil ||
		!strings.Contains(err.Error(), "yk-kai unlink") {
		t.Fatalf("без типа нужна подсказка про unlink, получено %v", err)
	}
	if _, _, _, err := linkArgs("unlink", []string{"PROJ-1", "blocks", "PROJ-2"}); err == nil {
		t.Fatal("незнакомый тип связи должен отвергаться до запроса")
	}
}

// Снятие сверяется числом: у задачи к той же цели бывает связь другого типа,
// и поиск «есть ли связь» счёл бы снятие несостоявшимся.
func TestCountLinksAcrossTypes(t *testing.T) {
	issue := &youtrack.Issue{Links: []youtrack.Link{
		{LinkType: youtrack.LinkType{Name: "Relates"}, Issues: []youtrack.Issue{{IDReadable: "PROJ-2"}}},
		{LinkType: youtrack.LinkType{Name: "Depend"}, Direction: "OUTWARD",
			Issues: []youtrack.Issue{{IDReadable: "proj-2"}, {IDReadable: "PROJ-3"}}},
	}}
	if got := countLinks(issue, "PROJ-2"); got != 2 {
		t.Fatalf("связей с PROJ-2: %d, ждали 2", got)
	}
	if countLinks(issue, "PROJ-9") != 0 || hasLink(issue, "PROJ-9") {
		t.Fatal("связи с PROJ-9 нет")
	}
}
