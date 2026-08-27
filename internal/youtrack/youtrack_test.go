package youtrack

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		allowed []string
		want    string
		wantErr bool
	}{
		{"как в API", "Bug", Types, "Bug", false},
		{"другой регистр", "bug", Types, "Bug", false},
		{"с пробелами", "  Major  ", Priorities, "Major", false},
		{"многословное", "in progress", States, "In Progress", false},
		{"русское из интерфейса", "Ошибка", Types, "Bug", false},
		{"русский приоритет", "обычная", Priorities, "Normal", false},
		{"русское состояние", "В работе", States, "In Progress", false},
		{"чужое поле", "Ошибка", Priorities, "", true},
		{"незнакомое", "Blocker", Priorities, "", true},
		{"пустое", "", Types, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize("поле", tc.value, tc.allowed)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ожидали ошибку, получили %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if got != tc.want {
				t.Fatalf("получили %q, ожидали %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeErrorListsAllowed(t *testing.T) {
	_, err := Normalize("Priority", "Blocker", Priorities)
	if err == nil {
		t.Fatal("ожидали ошибку")
	}
	// Список допустимых в тексте — единственный способ узнать значения, не
	// открывая YouTrack.
	if !strings.Contains(err.Error(), "Show-stopper") {
		t.Fatalf("в ошибке нет списка значений: %v", err)
	}
}

func TestDecodeErrorObjectWithHTTP200(t *testing.T) {
	// Гоча, ради которой существует decode: отказ приезжает объектом и с
	// кодом 200, и без проверки разбирается в пустую задачу.
	body := []byte(`{"error":"Bad Request","error_description":"Unknown field: Priorityy"}`)
	var issue Issue
	err := decode(200, body, &issue)
	if err == nil {
		t.Fatal("ожидали ошибку на отказ-объект")
	}
	if !strings.Contains(err.Error(), "Unknown field") {
		t.Fatalf("причина потерялась: %v", err)
	}
}

func TestDecodeStatuses(t *testing.T) {
	cases := []struct {
		status int
		kind   string
	}{
		{401, KindAuth},
		{403, KindAuth},
		{404, KindNotFound},
		{400, KindAPI},
	}
	for _, tc := range cases {
		err := decode(tc.status, []byte(`{"error":"x","error_description":"y"}`), nil)
		if err == nil {
			t.Fatalf("HTTP %d: ожидали ошибку", tc.status)
		}
		if got := KindOf(err); got != tc.kind {
			t.Fatalf("HTTP %d: класс %q, ожидали %q", tc.status, got, tc.kind)
		}
	}
}

func TestDecodeSuccess(t *testing.T) {
	var issue Issue
	if err := decode(200, []byte(`{"idReadable":"PROJ-1","summary":"тест"}`), &issue); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if issue.IDReadable != "PROJ-1" || issue.Summary != "тест" {
		t.Fatalf("разобралось не то: %+v", issue)
	}

	// Пустое тело — обычный ответ POST'ов YouTrack, это не ошибка.
	if err := decode(200, nil, &issue); err != nil {
		t.Fatalf("пустое тело считаем ошибкой: %v", err)
	}
}

func TestFieldValueRendering(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"enum", `{"name":"Major"}`, "Major"},
		{"пользователь", `{"login":"alice","fullName":"Alice"}`, "alice"},
		{"не заполнено", `null`, ""},
		{"число", `42`, "42"},
		{"строка", `"текст"`, "текст"},
		{"массив", `[{"name":"a"},{"name":"b"}]`, "a, b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := CustomField{Name: "X", Value: json.RawMessage(tc.raw)}
			if got := f.String(); got != tc.want {
				t.Fatalf("получили %q, ожидали %q", got, tc.want)
			}
		})
	}
}

func TestLinkSide(t *testing.T) {
	// Без direction обе стороны Subtask выглядят одинаково: «parent for» не
	// отличить от «subtask of».
	lt := LinkType{Name: "Subtask", SourceToTarget: "parent for", TargetToSource: "subtask of"}

	if got := (Link{Direction: "OUTWARD", LinkType: lt}).Side(); got != "parent for" {
		t.Fatalf("исходящая сторона: %q", got)
	}
	if got := (Link{Direction: "INWARD", LinkType: lt}).Side(); got != "subtask of" {
		t.Fatalf("входящая сторона: %q", got)
	}
	if got := (Link{LinkType: LinkType{Name: "Relates"}}).Side(); got != "Relates" {
		t.Fatalf("без направления: %q", got)
	}
}

func TestIssueField(t *testing.T) {
	issue := Issue{Fields: []CustomField{
		{Name: "State", Value: json.RawMessage(`{"name":"In Progress"}`)},
		{Name: "Assignee", Value: json.RawMessage(`{"login":"alice"}`)},
	}}
	if got := issue.Field("state"); got != "In Progress" {
		t.Fatalf("State: %q", got)
	}
	if got := issue.Field("Assignee"); got != "alice" {
		t.Fatalf("Assignee: %q", got)
	}
	if got := issue.Field("Priority"); got != "" {
		t.Fatalf("отсутствующее поле: %q", got)
	}
}

func TestStateFieldType(t *testing.T) {
	// State с $type SingleEnumIssueCustomField YouTrack принимает и молча не
	// применяет — тип поля здесь часть контракта.
	if StateField("Done").Type != "StateIssueCustomField" {
		t.Fatalf("не тот $type: %+v", StateField("Done"))
	}
	if EnumField("Priority", "Major").Type != "SingleEnumIssueCustomField" {
		t.Fatalf("не тот $type: %+v", EnumField("Priority", "Major"))
	}
	if UserField("alice").Type != "SingleUserIssueCustomField" {
		t.Fatalf("не тот $type: %+v", UserField("alice"))
	}
}

func TestEncodeQuerySpaces(t *testing.T) {
	// YouTrack не считает плюс пробелом и отвечает invalid_query на корректный
	// запрос — пробелы обязаны уезжать как %20.
	got := encodeQuery(url.Values{"query": []string{"project: PROJ #Unresolved"}})
	if strings.Contains(got, "+") {
		t.Fatalf("пробел закодирован плюсом: %s", got)
	}
	if !strings.Contains(got, "%20") {
		t.Fatalf("нет %%20 в запросе: %s", got)
	}
	// Настоящий плюс должен пережить замену.
	got = encodeQuery(url.Values{"query": []string{"a+b"}})
	if !strings.Contains(got, "%2B") {
		t.Fatalf("плюс из значения потерялся: %s", got)
	}
}
