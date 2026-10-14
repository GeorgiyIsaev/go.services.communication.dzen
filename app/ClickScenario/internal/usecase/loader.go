package usecase

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"go.services.communication.dzen/internal/ClickScenario/domain"
)

// LoadScenarios читает все *.scenario из папки dir и возвращает плоский
// список пользователей. В одном файле может быть несколько пользователей.
// Файлы обрабатываются в лексикографическом порядке имён.
func LoadScenarios(dir string) ([]domain.Scenario, error) {
	files, err := scenarioFiles(dir)
	if err != nil {
		return nil, err
	}

	var all []domain.Scenario
	for _, path := range files {
		scenarios, err := parseScenarioFile(path)
		if err != nil {
			return nil, fmt.Errorf("разбор %q: %w", path, err)
		}
		all = append(all, scenarios...)
	}
	return all, nil
}

// scenarioFiles возвращает отсортированный список путей к *.scenario в dir.
func scenarioFiles(dir string) ([]string, error) {
	if dir == "" {
		return nil, fmt.Errorf("не задан scenario_dir")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать папку со сценариями %q: %w", dir, err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), ".scenario") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("в папке %q не найдено ни одного *.scenario", dir)
	}

	sort.Strings(paths)
	return paths, nil
}

// parseScenarioFile читает один файл и собирает из него список пользователей.
func parseScenarioFile(path string) ([]domain.Scenario, error) {
	lines, err := readScenarioLines(path)
	if err != nil {
		return nil, err
	}
	return buildScenarios(lines)
}

// readScenarioLines возвращает значимые строки файла: обрезанные по краям,
// без пустых строк и строк-комментариев (начинающихся с "#").
// Каждая строка обёрнута в line{num, text}, чтобы ошибки ссылались на номер.
func readScenarioLines(path string) ([]line, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []line
	scanner := bufio.NewScanner(f)
	// Строка с 20 author_id легко укладывается, но запас не помешает.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	num := 0
	for scanner.Scan() {
		num++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		lines = append(lines, line{num: num, text: text})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// line — значимая строка файла с её оригинальным номером (для сообщений об ошибке).
type line struct {
	num  int
	text string
}

// buildScenarios проходит по строкам как по state-machine: строка "user_id"
// закрывает предыдущий блок и открывает новый, строка "authors" наполняет текущий.
func buildScenarios(lines []line) ([]domain.Scenario, error) {
	var (
		scenarios []domain.Scenario
		current   *domain.Scenario
	)

	flush := func() error {
		if current == nil {
			return nil
		}
		if err := current.Validate(); err != nil {
			return err
		}
		scenarios = append(scenarios, *current)
		current = nil
		return nil
	}

	for _, ln := range lines {
		key, value, err := splitKeyValue(ln.text)
		if err != nil {
			return nil, fmt.Errorf("строка %d: %w", ln.num, err)
		}

		switch key {
		case "user_id":
			if err := flush(); err != nil {
				return nil, fmt.Errorf("строка %d: %w", ln.num, err)
			}
			id, err := parseUserID(value)
			if err != nil {
				return nil, fmt.Errorf("строка %d: %w", ln.num, err)
			}
			current = &domain.Scenario{UserID: id}

		case "authors":
			if current == nil {
				return nil, fmt.Errorf("строка %d: authors указан до user_id", ln.num)
			}
			authors, err := parseAuthors(value)
			if err != nil {
				return nil, fmt.Errorf("строка %d: %w", ln.num, err)
			}
			current.Authors = authors

		default:
			// Неизвестные ключи молча игнорируем — удобно для расширения формата.
		}
	}

	if err := flush(); err != nil {
		return nil, err
	}
	if len(scenarios) == 0 {
		return nil, fmt.Errorf("в файле не найдено ни одного пользователя")
	}
	return scenarios, nil
}

// splitKeyValue разбирает строку вида "ключ: значение".
func splitKeyValue(text string) (key, value string, err error) {
	kv := strings.SplitN(text, ":", 2)
	if len(kv) != 2 {
		return "", "", fmt.Errorf("ожидался формат 'ключ: значение', получено %q", text)
	}
	return strings.ToLower(strings.TrimSpace(kv[0])), strings.TrimSpace(kv[1]), nil
}

// parseUserID парсит значение ключа user_id.
func parseUserID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("некорректный user_id %q: %w", value, err)
	}
	if id <= 0 {
		return 0, fmt.Errorf("user_id должен быть > 0, получено %d", id)
	}
	return id, nil
}

// parseAuthors парсит список author_id, разделённых запятыми и/или пробелами.
// Допускаются обрамляющие квадратные скобки: "[1, 2, 3]" == "1, 2, 3".
func parseAuthors(value string) ([]int64, error) {
	value = strings.Trim(value, "[]")
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})

	authors := make([]int64, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		a, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("некорректный author_id %q: %w", f, err)
		}
		if a <= 0 {
			return nil, fmt.Errorf("author_id должен быть > 0, получено %d", a)
		}
		authors = append(authors, a)
	}
	return authors, nil
}
