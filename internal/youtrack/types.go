package youtrack

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Issue — задача в том объёме, в каком её запрашивают команды CLI.
type Issue struct {
	ID          string        `json:"id"`
	IDReadable  string        `json:"idReadable"`
	Summary     string        `json:"summary"`
	Description string        `json:"description,omitempty"`
	Fields      []CustomField `json:"customFields,omitempty"`
	Comments    []Comment     `json:"comments,omitempty"`
	Links       []Link        `json:"links,omitempty"`
}

// CustomField — значение поля как его отдаёт YouTrack.
//
// Value держим сырым намеренно: у enum-полей это объект с name, у Assignee —
// объект с login, у Estimation — число, у множественных полей — массив.
// Строгая структура здесь ломается на первом же непривычном поле проекта.
type CustomField struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// String приводит значение поля к строке для вывода. Пустая строка означает
// «поле не заполнено» — так же, как null в ответе API.
func (f CustomField) String() string {
	return renderValue(f.Value)
}

type Comment struct {
	Author  User   `json:"author"`
	Created int64  `json:"created"`
	Text    string `json:"text"`
}

// CreatedAt переводит миллисекунды YouTrack в локальное время.
func (c Comment) CreatedAt() time.Time {
	return time.UnixMilli(c.Created)
}

type User struct {
	Login    string `json:"login"`
	FullName string `json:"fullName,omitempty"`
}

type Link struct {
	Direction string   `json:"direction"`
	LinkType  LinkType `json:"linkType"`
	Issues    []Issue  `json:"issues"`
}

type LinkType struct {
	Name           string `json:"name"`
	SourceToTarget string `json:"sourceToTarget"`
	TargetToSource string `json:"targetToSource"`
}

// Side — название связи со стороны этой задачи. Без учёта direction обе
// стороны Subtask выглядят одинаково, и «parent for» не отличить от
// «subtask of».
func (l Link) Side() string {
	if l.Direction == "OUTWARD" && l.LinkType.SourceToTarget != "" {
		return l.LinkType.SourceToTarget
	}
	if l.Direction == "INWARD" && l.LinkType.TargetToSource != "" {
		return l.LinkType.TargetToSource
	}
	if l.LinkType.Name != "" {
		return l.LinkType.Name
	}
	return "связи"
}

// Field возвращает значение поля по имени.
func (i Issue) Field(name string) string {
	for _, f := range i.Fields {
		if strings.EqualFold(f.Name, name) {
			return f.String()
		}
	}
	return ""
}

func renderValue(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}

	switch trimmed[0] {
	case '{':
		var v struct {
			Name         string `json:"name"`
			Login        string `json:"login"`
			FullName     string `json:"fullName"`
			Text         string `json:"text"`
			Presentation string `json:"presentation"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return trimmed
		}
		for _, candidate := range []string{v.Name, v.Login, v.FullName, v.Presentation, v.Text} {
			if candidate != "" {
				return candidate
			}
		}
		return ""
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return trimmed
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			if s := renderValue(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return trimmed
		}
		return s
	default:
		var n float64
		if err := json.Unmarshal(raw, &n); err == nil {
			return strconv.FormatFloat(n, 'f', -1, 64)
		}
		return trimmed
	}
}
