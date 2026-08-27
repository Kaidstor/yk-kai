// Package youtrack — клиент REST API YouTrack: запросы с ретраями, разбор
// отказов и типы задачи.
package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Kaidstor/yk-kai/internal/config"
)

// Kind — класс отказа. По нему вызывающий выбирает код выхода, не разбирая текст.
const (
	KindAuth     = "auth"
	KindNotFound = "not_found"
	KindTimeout  = "timeout"
	KindNetwork  = "network"
	KindAPI      = "api"
)

// APIError — отказ YouTrack в машинном виде.
type APIError struct {
	Kind    string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("YouTrack HTTP %d: %s", e.Status, e.Message)
	}
	return e.Message
}

// KindOf достаёт класс отказа из ошибки любой глубины вложенности.
func KindOf(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Kind
	}
	return KindNetwork
}

type Client struct {
	cfg   config.Config
	token string
	http  *http.Client
}

func New(cfg config.Config, token string) *Client {
	return &Client{
		cfg:   cfg,
		token: token,
		http:  &http.Client{Timeout: config.RequestTimeout},
	}
}

// Config — настройки, с которыми создан клиент.
func (c *Client) Config() config.Config { return c.cfg }

// GetJSON называется так, а не Get, чтобы не спорить с Client.Get — чтением
// задачи, которое зовут команды.
func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

func (c *Client) Post(ctx context.Context, path string, query url.Values, body, out any) error {
	return c.do(ctx, http.MethodPost, path, query, body, out)
}

// do отправляет запрос с ретраями. Ретраятся только сеть и 5xx: YouTrack
// периодически отвечает медленно и рвёт соединение, а все операции CLI
// идемпотентны, кроме создания задачи — там ретрай выключается вызывающим
// через контекст с коротким сроком.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("не удалось собрать тело запроса: %w", err)
		}
	}

	endpoint := c.cfg.Host + path
	if len(query) > 0 {
		endpoint += "?" + encodeQuery(query)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctxError(ctx)
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctxError(ctx)
			}
			lastErr = &APIError{Kind: KindNetwork, Message: err.Error()}
			continue
		}

		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = &APIError{Kind: KindNetwork, Message: readErr.Error()}
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = &APIError{Kind: KindAPI, Status: resp.StatusCode, Message: describe(raw)}
			continue
		}

		return decode(resp.StatusCode, raw, out)
	}

	return lastErr
}

// decode разбирает ответ. Гоча: отказ YouTrack приходит объектом
// {"error": ..., "error_description": ...}, причём иногда с HTTP 200 — без
// явной проверки он разбирается молча в пустую структуру, и команда рапортует
// успех там, где ничего не произошло.
func decode(status int, raw []byte, out any) error {
	if status >= 400 {
		return &APIError{Kind: kindByStatus(status), Status: status, Message: describe(raw)}
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}

	if bytes.HasPrefix(trimmed, []byte("{")) {
		var probe struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if err := json.Unmarshal(trimmed, &probe); err == nil && probe.Error != "" {
			msg := probe.Error
			if probe.Description != "" {
				msg += ": " + probe.Description
			}
			return &APIError{Kind: KindAPI, Status: status, Message: msg}
		}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(trimmed, out); err != nil {
		return &APIError{Kind: KindAPI, Status: status, Message: "неразбираемый ответ: " + describe(trimmed)}
	}
	return nil
}

// encodeQuery кодирует пробелы как %20, а не плюсом.
//
// Гоча: url.Values.Encode даёт `project:+FB`, а YouTrack плюс пробелом не
// считает и отвечает `invalid_query: Can't parse search query` на совершенно
// корректный запрос. Настоящий плюс к этому моменту уже закодирован в %2B,
// поэтому замена безопасна.
func encodeQuery(query url.Values) string {
	return strings.ReplaceAll(query.Encode(), "+", "%20")
}

func kindByStatus(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return KindAuth
	case http.StatusNotFound:
		return KindNotFound
	default:
		return KindAPI
	}
}

func ctxError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &APIError{Kind: KindTimeout, Message: "YouTrack не ответил вовремя"}
	}
	return &APIError{Kind: KindNetwork, Message: ctx.Err().Error()}
}

// describe вытаскивает из тела человекочитаемую суть: YouTrack кладёт её в
// error_description, а при 404 отвечает голым текстом.
func describe(raw []byte) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "пустой ответ"
	}

	var probe struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal([]byte(trimmed), &probe); err == nil {
		switch {
		case probe.Description != "" && probe.Error != "":
			return probe.Error + ": " + probe.Description
		case probe.Description != "":
			return probe.Description
		case probe.Error != "":
			return probe.Error
		}
	}

	if len(trimmed) > 300 {
		return trimmed[:300] + "…"
	}
	return trimmed
}
