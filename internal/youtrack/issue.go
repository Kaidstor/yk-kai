package youtrack

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Наборы полей запроса. YouTrack по умолчанию отдаёт голый id, поэтому всё,
// что нужно показать, перечисляется явно.
const (
	FieldsShort = "id,idReadable,summary,customFields(name,value(name,login,fullName,presentation,text))"
	FieldsFull  = FieldsShort + ",description" +
		",comments(author(login,fullName),created,text)" +
		",links(direction,linkType(name,sourceToTarget,targetToSource)" +
		",issues(idReadable,summary,customFields(name,value(name))))"
	FieldsLinks = "id,idReadable,links(direction,linkType(name,sourceToTarget,targetToSource)" +
		",issues(idReadable,summary,customFields(name,value(name))))"
)

// CreateRequest — тело создания задачи.
type CreateRequest struct {
	Project     string        `json:"-"`
	Summary     string        `json:"summary"`
	Description string        `json:"description,omitempty"`
	Fields      []FieldUpdate `json:"-"`
}

// Create заводит задачу. Возвращает её с внутренним id — он нужен, чтобы
// положить задачу на доску.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*Issue, error) {
	body := map[string]any{
		"project":     map[string]string{"shortName": req.Project},
		"summary":     req.Summary,
		"description": req.Description,
	}
	if len(req.Fields) > 0 {
		body["customFields"] = req.Fields
	}

	var issue Issue
	query := url.Values{"fields": []string{"id,idReadable"}}
	if err := c.Post(ctx, "/api/issues", query, body, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// Get читает задачу. full добавляет описание, комментарии и связи.
func (c *Client) Get(ctx context.Context, id string, full bool) (*Issue, error) {
	fields := FieldsShort
	if full {
		fields = FieldsFull
	}
	var issue Issue
	query := url.Values{"fields": []string{fields}}
	if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(id), query, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// Links читает только связи задачи.
func (c *Client) Links(ctx context.Context, id string) (*Issue, error) {
	var issue Issue
	query := url.Values{"fields": []string{FieldsLinks}}
	if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(id), query, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// Update правит поля задачи. Гоча: YouTrack отвечает {"id": ...} и на запрос,
// который ничего не изменил, поэтому вызывающий обязан перечитать задачу и
// сверить — успешный ответ здесь не доказательство.
func (c *Client) Update(ctx context.Context, id string, fields []FieldUpdate, summary, description *string) error {
	body := map[string]any{}
	if len(fields) > 0 {
		body["customFields"] = fields
	}
	if summary != nil {
		body["summary"] = *summary
	}
	if description != nil {
		body["description"] = *description
	}
	if len(body) == 0 {
		return nil
	}
	return c.Post(ctx, "/api/issues/"+url.PathEscape(id), nil, body, nil)
}

// Comment добавляет комментарий.
func (c *Client) Comment(ctx context.Context, id, text string) error {
	return c.Post(ctx, "/api/issues/"+url.PathEscape(id)+"/comments", nil,
		map[string]string{"text": text}, nil)
}

// Board кладёт задачу в спринт доски из настроек. Идемпотентно: повторный
// вызов для уже прицепленной задачи проходит без ошибки.
func (c *Client) Board(ctx context.Context, internalID string) error {
	if !c.cfg.BoardConfigured() {
		return &APIError{Kind: KindAPI, Message: "доска не настроена: заполни agile_id и sprint_id"}
	}
	path := "/api/agiles/" + c.cfg.AgileID + "/sprints/" + c.cfg.SprintID + "/issues"
	return c.Post(ctx, path, nil, map[string]string{"id": internalID}, nil)
}

// Search выполняет поисковый запрос YouTrack.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Issue, error) {
	if limit <= 0 {
		limit = 20
	}
	values := url.Values{
		"query":  []string{query},
		"fields": []string{FieldsShort},
		"$top":   []string{strconv.Itoa(limit)},
	}
	var issues []Issue
	if err := c.GetJSON(ctx, "/api/issues", values, &issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// Command выполняет команду YouTrack над задачами — этим API ставятся связи.
// Прямые эндпоинты links требуют id типа связи и различают стороны, здесь
// достаточно текста: "depends on PROJ-823", "subtask of PROJ-755".
func (c *Client) Command(ctx context.Context, query string, issues []string) error {
	list := make([]map[string]string, 0, len(issues))
	for _, id := range issues {
		list = append(list, map[string]string{"idReadable": id})
	}
	return c.Post(ctx, "/api/commands", nil, map[string]any{
		"query":  query,
		"issues": list,
	}, nil)
}

// WorkItem списывает время. duration — в записи YouTrack ("2h", "45m", "1d 2h").
func (c *Client) WorkItem(ctx context.Context, id, duration, text string, date time.Time) error {
	body := map[string]any{
		"duration": map[string]string{"presentation": duration},
		"date":     date.UnixMilli(),
	}
	if text != "" {
		body["text"] = text
	}
	return c.Post(ctx, "/api/issues/"+url.PathEscape(id)+"/timeTracking/workItems", nil, body, nil)
}

// Me — под каким пользователем работает токен.
func (c *Client) Me(ctx context.Context) (User, error) {
	var user User
	query := url.Values{"fields": []string{"login,fullName"}}
	err := c.GetJSON(ctx, "/api/users/me", query, &user)
	return user, err
}

// ProjectExists проверяет, что проект доступен этому токену.
func (c *Client) ProjectExists(ctx context.Context, shortName string) (bool, error) {
	var projects []struct {
		ShortName string `json:"shortName"`
	}
	query := url.Values{
		"fields": []string{"shortName"},
		"$top":   []string{"200"},
	}
	if err := c.GetJSON(ctx, "/api/admin/projects", query, &projects); err != nil {
		return false, err
	}
	for _, p := range projects {
		if p.ShortName == shortName {
			return true, nil
		}
	}
	return false, nil
}

// SprintExists проверяет, что доска и спринт из настроек на месте.
func (c *Client) SprintExists(ctx context.Context) (string, error) {
	if !c.cfg.BoardConfigured() {
		return "", &APIError{Kind: KindAPI, Message: "доска не настроена"}
	}
	var sprint struct {
		Name  string `json:"name"`
		Agile struct {
			Name string `json:"name"`
		} `json:"agile"`
	}
	path := "/api/agiles/" + c.cfg.AgileID + "/sprints/" + c.cfg.SprintID
	query := url.Values{"fields": []string{"name,agile(name)"}}
	if err := c.GetJSON(ctx, path, query, &sprint); err != nil {
		return "", err
	}
	return sprint.Agile.Name + " / " + sprint.Name, nil
}
