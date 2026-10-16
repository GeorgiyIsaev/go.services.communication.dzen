package service

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
)

// LoadScenarios читает все *.scenario из папки dir.
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

func parseScenarioFile(path string) ([]domain.Scenario, error) {
	lines, err := readScenarioLines(path)
	if err != nil {
		return nil, err
	}
	return buildScenarios(lines)
}

type line struct {
	num  int
	text string
}

func readScenarioLines(path string) ([]line, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []line
	scanner := bufio.NewScanner(f)
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
		switch {
		case strings.HasPrefix(ln.text, "user_id"):
			if err := flush(); err != nil {
				return nil, err
			}
			val := strings.TrimSpace(strings.TrimPrefix(ln.text, "user_id"))
			val = strings.TrimPrefix(val, ":")
			val = strings.TrimSpace(val)
			id, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("строка %d: неверный user_id %q: %w", ln.num, val, err)
			}
			if id <= 0 {
				return nil, fmt.Errorf("строка %d: user_id должен быть положительным, получено %d", ln.num, id)
			}
			current = &domain.Scenario{UserID: id}
		case strings.HasPrefix(ln.text, "authors"):
			if current == nil {
				return nil, fmt.Errorf("строка %d: authors без user_id", ln.num)
			}
			val := strings.TrimSpace(strings.TrimPrefix(ln.text, "authors"))
			val = strings.TrimPrefix(val, ":")
			for _, p := range strings.Split(val, ",") {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				id, err := strconv.ParseInt(p, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("строка %d: неверный author_id %q: %w", ln.num, p, err)
				}
				if id <= 0 {
					return nil, fmt.Errorf("строка %d: author_id должен быть положительным, получено %d", ln.num, id)
				}
				current.Authors = append(current.Authors, id)
			}
		default:
			if isComment(ln.text) {
				continue
			}
			return nil, fmt.Errorf("строка %d: неизвестная директива %q", ln.num, ln.text)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return scenarios, nil
}

func isComment(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) && r != '#' {
			return false
		}
	}
	return true
}
