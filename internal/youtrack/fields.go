package youtrack

import (
	"fmt"
	"strings"
)

// Поля проекта FB и их допустимые значения.
//
// Гоча: в интерфейсе YouTrack значения показаны по-русски («Ошибка»,
// «Обычная»), а API принимает только английские. Поэтому CLI знает и русские
// написания — иначе агент, скопировавший значение из карточки задачи, получал
// бы отказ без объяснения.
var (
	Types = []string{
		"Bug", "Cosmetics", "Exception", "Feature", "Task",
		"Usability Problem", "Performance Problem", "Epic",
	}
	Priorities = []string{"Show-stopper", "Critical", "Major", "Normal", "Minor"}
	States     = []string{
		"Backlog", "To Do", "In Progress", "Review", "Testing & Deploy", "Done", "Archived",
	}
)

var russian = map[string]string{
	// Type
	"ошибка":             "Bug",
	"баг":                "Bug",
	"косметика":          "Cosmetics",
	"исключение":         "Exception",
	"функциональность":   "Feature",
	"фича":               "Feature",
	"задача":             "Task",
	"проблема юзабилити": "Usability Problem",
	"проблема производительности": "Performance Problem",
	"эпик": "Epic",
	// Priority
	"блокирующая":    "Show-stopper",
	"критическая":    "Critical",
	"важная":         "Major",
	"обычная":        "Normal",
	"незначительная": "Minor",
	// State
	"бэклог":   "Backlog",
	"сделать":  "To Do",
	"в работе": "In Progress",
	"ревью":    "Review",
	"тестирование и деплой": "Testing & Deploy",
	"готово": "Done",
	"архив":  "Archived",
}

// FieldUpdate — одно поле в теле POST /api/issues/<id>.
type FieldUpdate struct {
	Name  string `json:"name"`
	Type  string `json:"$type"`
	Value any    `json:"value"`
}

// EnumField собирает обновление enum-поля (Type, Priority).
func EnumField(name, value string) FieldUpdate {
	return FieldUpdate{Name: name, Type: "SingleEnumIssueCustomField", Value: map[string]string{"name": value}}
}

// StateField собирает обновление State: у него собственный $type, и с
// SingleEnumIssueCustomField YouTrack молча ничего не меняет.
func StateField(value string) FieldUpdate {
	return FieldUpdate{Name: "State", Type: "StateIssueCustomField", Value: map[string]string{"name": value}}
}

// UserField собирает обновление Assignee — значение задаётся логином.
func UserField(login string) FieldUpdate {
	return FieldUpdate{Name: "Assignee", Type: "SingleUserIssueCustomField", Value: map[string]string{"login": login}}
}

// Normalize приводит значение поля к тому написанию, которое принимает API:
// правит регистр, понимает русские названия из интерфейса. Незнакомое
// значение — ошибка со списком допустимых, а не молчаливый отказ на стороне
// YouTrack.
func Normalize(field, value string, allowed []string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", fmt.Errorf("пустое значение поля %s", field)
	}

	for _, a := range allowed {
		if strings.EqualFold(a, v) {
			return a, nil
		}
	}

	if en, ok := russian[strings.ToLower(v)]; ok {
		for _, a := range allowed {
			if a == en {
				return a, nil
			}
		}
	}

	return "", fmt.Errorf("недопустимое значение %s: %q; допустимы: %s", field, value, strings.Join(allowed, ", "))
}
