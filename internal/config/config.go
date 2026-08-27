// Package config — настройки инстанса YouTrack и поиск токена.
//
// В коде нет ни адреса инстанса, ни проекта, ни доски: всё это читается из
// файла настроек (`~/.config/yk-kai/config.json`) и переопределяется
// переменными окружения. Так один бинарь годится любому инстансу, а рабочие
// значения не уезжают в репозиторий.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RequestTimeout щедрый намеренно: agile-эндпоинты YouTrack регулярно отвечают
// секунд по двадцать, и таймаут здесь означал бы «повтори то, что на самом деле
// уже применилось».
const RequestTimeout = 45 * time.Second

// Config — то, что различается между инстансами и людьми.
type Config struct {
	// Host — базовый адрес инстанса, например https://example.youtrack.cloud
	Host string `json:"host"`
	// Project — короткое имя проекта по умолчанию, например PROJ
	Project string `json:"project"`
	// Assignee — логин, на который вешаются задачи, если не сказано иное
	Assignee string `json:"assignee"`
	// AgileID и SprintID — доска и спринт: созданная через API задача на доску
	// сама не попадает. Пустые — привязка отключена.
	AgileID  string `json:"agile_id"`
	SprintID string `json:"sprint_id"`
	// TokenRef — ссылка на секрет в sec, вида "<проект>/<KEY>"
	TokenRef string `json:"token_ref"`

	// Path — откуда прочитан файл; пустой, если настройки только из окружения.
	Path string `json:"-"`
}

// Token — значение и то, откуда оно взялось. Источник нужен doctor'у: когда
// YouTrack отвечает 401, первый вопрос — какой именно токен подставился.
type Token struct {
	Value  string
	Source string
}

// ErrNoConfig — файла настроек нет и окружение пустое.
var ErrNoConfig = errors.New("настройки не найдены")

// Load читает настройки: файл, поверх него — переменные окружения.
func Load() (Config, error) {
	path := Path()

	var cfg Config
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("%s: не разобрать JSON: %w", path, err)
		}
		cfg.Path = path
	case !errors.Is(err, os.ErrNotExist):
		return Config{}, err
	}

	applyEnv(&cfg)

	if cfg.Host == "" || cfg.Project == "" {
		return cfg, fmt.Errorf(
			"%w: заполни host и project в %s (создать шаблон: yk-kai config init)", ErrNoConfig, path)
	}

	cfg.Host = strings.TrimRight(cfg.Host, "/")
	return cfg, nil
}

// Path — путь к файлу настроек: $YK_KAI_CONFIG, иначе $XDG_CONFIG_HOME или
// ~/.config.
//
// Гоча: os.UserConfigDir на macOS отдаёт ~/Library/Application Support, и файл
// оказывается не там, где его ищут руками и где обещает README.
func Path() string {
	if v := strings.TrimSpace(os.Getenv("YK_KAI_CONFIG")); v != "" {
		return v
	}
	dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".config", "yk-kai", "config.json")
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "yk-kai", "config.json")
}

// Template — заготовка файла настроек.
func Template() Config {
	return Config{
		Host:     "https://example.youtrack.cloud",
		Project:  "PROJ",
		Assignee: "",
		AgileID:  "",
		SprintID: "",
		TokenRef: "",
	}
}

// Save пишет настройки в файл, создавая каталог.
func Save(cfg Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

// BoardConfigured — заданы ли доска и спринт.
func (c Config) BoardConfigured() bool { return c.AgileID != "" && c.SprintID != "" }

// IssueURL — ссылка на задачу для человека.
func (c Config) IssueURL(id string) string { return c.Host + "/issue/" + id }

// LoadToken ищет токен: переменные окружения, затем sec по ссылке из настроек.
// Значение никуда не печатается — наружу отдаётся только источник и маска.
func (c Config) LoadToken() (Token, error) {
	for _, key := range []string{"YOUTRACK_TOKEN", "YOUTRACK_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return Token{Value: v, Source: "env:" + key}, nil
		}
	}

	if c.TokenRef != "" {
		if v, err := fromSec(c.TokenRef); err == nil && v != "" {
			return Token{Value: v, Source: "sec:" + c.TokenRef}, nil
		} else if err != nil {
			return Token{}, fmt.Errorf("sec %s: %w", c.TokenRef, err)
		}
	}

	return Token{}, fmt.Errorf(
		"токен не найден: задай $YOUTRACK_TOKEN или token_ref в %s", Path())
}

// Mask отдаёт представление, безопасное для чата и логов.
func Mask(value string) string {
	r := []rune(value)
	if len(r) < 8 {
		return fmt.Sprintf("(%d символов)", len(r))
	}
	return fmt.Sprintf("%s…%s (%d символов)", string(r[:2]), string(r[len(r)-2:]), len(r))
}

func applyEnv(cfg *Config) {
	for _, pair := range []struct {
		env    string
		target *string
	}{
		{"YOUTRACK_HOST", &cfg.Host},
		{"YOUTRACK_PROJECT", &cfg.Project},
		{"YOUTRACK_ASSIGNEE", &cfg.Assignee},
		{"YOUTRACK_AGILE_ID", &cfg.AgileID},
		{"YOUTRACK_SPRINT_ID", &cfg.SprintID},
		{"YOUTRACK_TOKEN_REF", &cfg.TokenRef},
	} {
		if v := strings.TrimSpace(os.Getenv(pair.env)); v != "" {
			*pair.target = v
		}
	}
}

func fromSec(ref string) (string, error) {
	bin, err := exec.LookPath("sec")
	if err != nil {
		return "", fmt.Errorf("sec не найден в PATH: %w", err)
	}
	out, err := exec.Command(bin, "get", ref).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
