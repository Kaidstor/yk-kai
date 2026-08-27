package command

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestDurationPattern(t *testing.T) {
	for _, ok := range []string{"2h", "45m", "1d 2h", "1w", "1d 2h 30m"} {
		if !duration.MatchString(ok) {
			t.Fatalf("%q должно приниматься", ok)
		}
	}
	for _, bad := range []string{"2 часа", "2", "h2", "2 hours"} {
		if duration.MatchString(bad) {
			t.Fatalf("%q не должно приниматься", bad)
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
	fs := flag.NewFlagSet("worklog", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	date := fs.String("date", "", "")

	positional, err := parseArgs(fs, []string{"PROJ-1", "--date", "2026-01-02", "2h", "текст"})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if *date != "2026-01-02" {
		t.Fatalf("--date не разобрался: %q", *date)
	}
	if strings.Join(positional, "|") != "PROJ-1|2h|текст" {
		t.Fatalf("порядок позиционных нарушен: %v", positional)
	}
}

func TestWorkDateSurvivesTimezone(t *testing.T) {
	// Гоча: YouTrack округляет метку списания до дня по UTC, и полночь по
	// местному времени восточнее Гринвича уезжала на сутки назад.
	msk := time.FixedZone("MSK", 3*60*60)
	day := time.Date(2026, 8, 26, 0, 0, 0, 0, msk)

	got := workDate(day)
	if got.UTC().Format("2006-01-02") != "2026-08-26" {
		t.Fatalf("дата уехала: %s", got.UTC())
	}

	// И на запад тоже: полдень UTC не должен скатиться на предыдущий день.
	pdt := time.FixedZone("PDT", -7*60*60)
	got = workDate(time.Date(2026, 8, 26, 23, 30, 0, 0, pdt))
	if got.UTC().Format("2006-01-02") != "2026-08-26" {
		t.Fatalf("дата уехала: %s", got.UTC())
	}
}
