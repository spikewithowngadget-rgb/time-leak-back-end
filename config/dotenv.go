package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open .env: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, ok, err := parseDotEnvLine(scanner.Text())
		if err != nil {
			return fmt.Errorf("parse .env line %d: %w", lineNo, err)
		}
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set .env key %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read .env: %w", err)
	}
	return nil
}

func parseDotEnvLine(line string) (string, string, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false, errors.New("missing '='")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, errors.New("empty key")
	}
	for _, r := range key {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", "", false, fmt.Errorf("invalid key %q", key)
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return key, "", true, nil
	}
	if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `'`) {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", "", false, err
		}
		return key, unquoted, true, nil
	}

	if beforeComment, _, hasComment := strings.Cut(value, " #"); hasComment {
		value = strings.TrimSpace(beforeComment)
	}
	return key, value, true, nil
}
