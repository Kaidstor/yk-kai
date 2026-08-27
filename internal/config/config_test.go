package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YK_KAI_CONFIG", path)
	return path
}

func TestLoadFromFile(t *testing.T) {
	path := writeConfig(t, `{
	  "host": "https://example.youtrack.cloud/",
	  "project": "PROJ",
	  "assignee": "alice",
	  "agile_id": "1-2",
	  "sprint_id": "3-4",
	  "token_ref": "work/YOUTRACK_API_KEY"
	}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	// Хвостовой слэш обязан отвалиться, иначе все пути уезжают с двойным.
	if cfg.Host != "https://example.youtrack.cloud" {
		t.Fatalf("host: %q", cfg.Host)
	}
	if cfg.Path != path {
		t.Fatalf("path: %q", cfg.Path)
	}
	if !cfg.BoardConfigured() {
		t.Fatal("доска задана, но BoardConfigured отдаёт false")
	}
	if got := cfg.IssueURL("PROJ-1"); got != "https://example.youtrack.cloud/issue/PROJ-1" {
		t.Fatalf("IssueURL: %q", got)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	writeConfig(t, `{"host":"https://from-file.example","project":"FILE"}`)
	t.Setenv("YOUTRACK_PROJECT", "ENV")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if cfg.Project != "ENV" {
		t.Fatalf("окружение не перебило файл: %q", cfg.Project)
	}
	if cfg.Host != "https://from-file.example" {
		t.Fatalf("host из файла потерялся: %q", cfg.Host)
	}
}

func TestLoadWithoutConfig(t *testing.T) {
	t.Setenv("YK_KAI_CONFIG", filepath.Join(t.TempDir(), "нет-такого.json"))

	_, err := Load()
	if err == nil {
		t.Fatal("без host и project настройки не годятся")
	}
	// По этой ошибке команда config отличает «не настроено» от поломки.
	if !errorIs(err, ErrNoConfig) {
		t.Fatalf("не тот класс ошибки: %v", err)
	}
}

func TestBoardNotConfigured(t *testing.T) {
	writeConfig(t, `{"host":"https://example.youtrack.cloud","project":"PROJ"}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if cfg.BoardConfigured() {
		t.Fatal("доска не задана, но BoardConfigured отдаёт true")
	}
}

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("YOUTRACK_TOKEN", "secret-value")

	token, err := Config{}.LoadToken()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if token.Source != "env:YOUTRACK_TOKEN" {
		t.Fatalf("источник: %q", token.Source)
	}
	// Значение токена нигде не печатается — наружу уходит только маска.
	if got := Mask(token.Value); got == token.Value {
		t.Fatalf("маска совпала со значением: %q", got)
	}
}

func TestMaskShortValue(t *testing.T) {
	if got := Mask("abc"); got != "(3 символов)" {
		t.Fatalf("короткое значение не должно светиться: %q", got)
	}
}

func errorIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestPathPrefersDotConfig(t *testing.T) {
	// Гоча macOS: os.UserConfigDir отдал бы ~/Library/Application Support, и
	// файл оказался бы не там, где его ищут руками и обещает README.
	t.Setenv("YK_KAI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("нет домашнего каталога")
	}
	want := filepath.Join(home, ".config", "yk-kai", "config.json")
	if got := Path(); got != want {
		t.Fatalf("получили %q, ожидали %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	if got := Path(); got != "/tmp/cfg/yk-kai/config.json" {
		t.Fatalf("XDG_CONFIG_HOME не учтён: %q", got)
	}
}
