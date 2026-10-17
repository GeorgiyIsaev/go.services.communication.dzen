package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
)

// --- helpers ---

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", name, err)
	}
	return path
}

func scenariosEqual(a, b []domain.Scenario) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].UserID != b[i].UserID {
			return false
		}
		if !int64SlicesEqual(a[i].Authors, b[i].Authors) {
			return false
		}
	}
	return true
}

func int64SlicesEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- buildScenarios ---

func TestBuildScenarios(t *testing.T) {
	tests := []struct {
		name    string
		lines   []line
		want    []domain.Scenario
		wantErr bool
	}{
		{
			name: "один сценарий",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: 2, 3"},
			},
			want: []domain.Scenario{{UserID: 1, Authors: []int64{2, 3}}},
		},
		{
			name: "несколько сценариев",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: 10"},
				{num: 3, text: "user_id: 2"},
				{num: 4, text: "authors: 20, 30"},
			},
			want: []domain.Scenario{
				{UserID: 1, Authors: []int64{10}},
				{UserID: 2, Authors: []int64{20, 30}},
			},
		},
		{
			name:  "пустой список",
			lines: nil,
			want:  nil,
		},
		{
			name: "без двоеточия",
			lines: []line{
				{num: 1, text: "user_id 1"},
				{num: 2, text: "authors 2, 3"},
			},
			want: []domain.Scenario{{UserID: 1, Authors: []int64{2, 3}}},
		},
		{
			name: "несколько директив authors склеиваются",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: 2, 3"},
				{num: 3, text: "authors: 4, 5"},
			},
			want: []domain.Scenario{{UserID: 1, Authors: []int64{2, 3, 4, 5}}},
		},
		{
			name: "authors с пустыми частями",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: , 2, , 3, "},
			},
			want: []domain.Scenario{{UserID: 1, Authors: []int64{2, 3}}},
		},
		{
			name: "authors без user_id",
			lines: []line{
				{num: 1, text: "authors: 1, 2"},
			},
			wantErr: true,
		},
		{
			name: "неверный user_id",
			lines: []line{
				{num: 1, text: "user_id: abc"},
			},
			wantErr: true,
		},
		{
			name: "неверный author_id",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: 1, x"},
			},
			wantErr: true,
		},
		{
			name: "неизвестная директива",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "unknown: value"},
			},
			wantErr: true,
		},
		{
			name: "очень большое переполнение int64 в author_id",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: 99999999999999999999"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildScenarios(tt.lines)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ожидалась ошибка, получено %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if !scenariosEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Отдельно — проверка, что доменная валидация вызывается.
// ВАЖНО: тесты ниже опираются на контракт domain.Scenario.Validate()
// (запрещает user_id <= 0, пустой authors, отрицательные author_id).
// Если у тебя Validate правила другие — поправь/удали эти кейсы.
func TestBuildScenarios_Validation(t *testing.T) {
	tests := []struct {
		name  string
		lines []line
	}{
		{
			name: "user_id без authors",
			lines: []line{
				{num: 1, text: "user_id: 1"},
			},
		},
		{
			name: "нулевой user_id",
			lines: []line{
				{num: 1, text: "user_id: 0"},
				{num: 2, text: "authors: 1"},
			},
		},
		{
			name: "отрицательный author_id",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: -1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildScenarios(tt.lines); err == nil {
				t.Fatalf("ожидалась ошибка валидации")
			}
		})
	}
}

// Проверяем, что ошибки отрицательных/нулевых id содержат номер строки.
func TestBuildScenarios_NegativeIDsWithLineNumber(t *testing.T) {
	tests := []struct {
		name      string
		lines     []line
		wantInErr string // подстрока, которую хотим увидеть в ошибке
	}{
		{
			name: "отрицательный author_id",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: 10, -1"},
			},
			wantInErr: "строка 2: author_id должен быть положительным, получено -1",
		},
		{
			name: "нулевой author_id",
			lines: []line{
				{num: 1, text: "user_id: 1"},
				{num: 2, text: "authors: 0"},
			},
			wantInErr: "строка 2: author_id должен быть положительным, получено 0",
		},
		{
			name: "отрицательный user_id",
			lines: []line{
				{num: 5, text: "user_id: -3"},
				{num: 6, text: "authors: 1"},
			},
			wantInErr: "строка 5: user_id должен быть положительным, получено -3",
		},
		{
			name: "нулевой user_id",
			lines: []line{
				{num: 7, text: "user_id: 0"},
				{num: 8, text: "authors: 1"},
			},
			wantInErr: "строка 7: user_id должен быть положительным, получено 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildScenarios(tt.lines)
			if err == nil {
				t.Fatal("ожидалась ошибка, получено nil")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("err = %q, want substring %q", err.Error(), tt.wantInErr)
			}
		})
	}
}

// --- scenarioFiles ---

func TestScenarioFiles(t *testing.T) {
	t.Run("пустой параметр", func(t *testing.T) {
		if _, err := scenarioFiles(""); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("несуществующая папка", func(t *testing.T) {
		if _, err := scenarioFiles("/this/path/does/not/exist/xyz123"); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("нет .scenario файлов", func(t *testing.T) {
		dir := t.TempDir()
		writeTempFile(t, dir, "notes.txt", "hello")
		if _, err := scenarioFiles(dir); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("сортировка, регистр расширения, подпапки игнорируются", func(t *testing.T) {
		dir := t.TempDir()
		writeTempFile(t, dir, "b.scenario", "")
		writeTempFile(t, dir, "a.scenario", "")
		writeTempFile(t, dir, "c.SCENARIO", "")
		writeTempFile(t, dir, "ignore.txt", "")
		if err := os.Mkdir(filepath.Join(dir, "sub.scenario"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		got, err := scenarioFiles(dir)
		if err != nil {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
		want := []string{
			filepath.Join(dir, "a.scenario"),
			filepath.Join(dir, "b.scenario"),
			filepath.Join(dir, "c.SCENARIO"),
		}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	})
}

// --- readScenarioLines ---

func TestReadScenarioLines(t *testing.T) {
	dir := t.TempDir()
	content := "# это комментарий\n" +
		"user_id: 1\n" +
		"   \n" +
		"authors: 2, 3\n" +
		"\n" +
		"# ещё комментарий\n" +
		"user_id: 4\n" +
		"authors: 5\n"
	path := writeTempFile(t, dir, "test.scenario", content)

	got, err := readScenarioLines(path)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	want := []line{
		{num: 2, text: "user_id: 1"},
		{num: 4, text: "authors: 2, 3"},
		{num: 7, text: "user_id: 4"},
		{num: 8, text: "authors: 5"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadScenarioLines_MissingFile(t *testing.T) {
	if _, err := readScenarioLines("/nope/does-not-exist.scenario"); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

// --- LoadScenarios ---

func TestLoadScenarios(t *testing.T) {
	t.Run("успешная загрузка нескольких файлов", func(t *testing.T) {
		dir := t.TempDir()
		writeTempFile(t, dir, "b.scenario", "user_id: 2\nauthors: 20\n")
		writeTempFile(t, dir, "a.scenario", "user_id: 1\nauthors: 10, 11\n")

		got, err := LoadScenarios(dir)
		if err != nil {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
		want := []domain.Scenario{
			{UserID: 1, Authors: []int64{10, 11}},
			{UserID: 2, Authors: []int64{20}},
		}
		if !scenariosEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("ошибка во втором файле", func(t *testing.T) {
		dir := t.TempDir()
		writeTempFile(t, dir, "a.scenario", "user_id: 1\nauthors: 10\n")
		writeTempFile(t, dir, "b.scenario", "неизвестная строка\n")

		if _, err := LoadScenarios(dir); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("пустой dir", func(t *testing.T) {
		if _, err := LoadScenarios(""); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("пустая папка", func(t *testing.T) {
		if _, err := LoadScenarios(t.TempDir()); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})
}
