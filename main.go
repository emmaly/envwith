package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode"
)

func main() {
	args := os.Args[1:]
	envFile := ".env"

	// Parse flags
	i := 0
	for i < len(args) {
		if args[i] == "-f" {
			if i+1 >= len(args) {
				fatal("flag -f requires a filename argument")
			}
			envFile = args[i+1]
			i += 2
			continue
		}
		if args[i] == "--" {
			i++
			break
		}
		break
	}

	cmdArgs := args[i:]
	if len(cmdArgs) == 0 {
		fatal("usage: envwith [-f FILE] [--] command [args...]")
	}

	f, err := os.Open(envFile)
	if err != nil {
		fatal("open %s: %v", envFile, err)
	}
	defer f.Close()

	vars, err := parseEnvFile(f, os.Environ())
	if err != nil {
		fatal("parse %s: %v", envFile, err)
	}

	// Build environment: start with current env, overlay parsed vars
	env := environToMap(os.Environ())
	for k, v := range vars {
		env[k] = v
	}
	envSlice := make([]string, 0, len(env))
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}

	binary, err := lookPath(cmdArgs[0])
	if err != nil {
		fatal("%v", err)
	}

	err = syscall.Exec(binary, cmdArgs, envSlice)
	fatal("exec %s: %v", binary, err)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "envwith: "+format+"\n", args...)
	os.Exit(1)
}

// lookPath finds the absolute path of a command, searching PATH if needed.
func lookPath(cmd string) (string, error) {
	if strings.Contains(cmd, "/") {
		return cmd, nil
	}
	path := os.Getenv("PATH")
	for _, dir := range strings.Split(path, ":") {
		if dir == "" {
			dir = "."
		}
		p := dir + "/" + cmd
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("command not found: %s", cmd)
}

// parseEnvFile reads an env file and returns a map of key→value with substitution applied.
func parseEnvFile(r io.Reader, environ []string) (map[string]string, error) {
	inherited := environToMap(environ)
	vars := make(map[string]string) // vars defined in file (in order of appearance)

	// lookup checks file-defined vars first, then inherited env
	lookup := func(name string) (string, bool) {
		if v, ok := vars[name]; ok {
			return v, true
		}
		if v, ok := inherited[name]; ok {
			return v, true
		}
		return "", false
	}

	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)

		// Skip blank lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Strip optional "export " prefix
		trimmed = strings.TrimPrefix(trimmed, "export ")

		eqIdx := strings.IndexByte(trimmed, '=')
		if eqIdx < 0 {
			return nil, fmt.Errorf("line %d: missing '='", lineNum)
		}

		key := strings.TrimSpace(trimmed[:eqIdx])
		rawVal := trimmed[eqIdx+1:]

		val, err := parseValue(rawVal, lookup)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		vars[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return vars, nil
}

// parseValue handles unquoted, single-quoted, and double-quoted values.
func parseValue(raw string, lookup func(string) (string, bool)) (string, error) {
	raw = strings.TrimLeftFunc(raw, unicode.IsSpace)

	if raw == "" {
		return "", nil
	}

	switch raw[0] {
	case '\'':
		// Single-quoted: literal, no substitution
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("unterminated single quote")
		}
		return raw[1 : end+1], nil

	case '"':
		// Double-quoted: substitution + escapes
		val, err := parseDoubleQuoted(raw[1:], lookup)
		if err != nil {
			return "", err
		}
		return val, nil

	default:
		// Unquoted: trim trailing comment, expand vars
		val := stripInlineComment(raw)
		val = strings.TrimRightFunc(val, unicode.IsSpace)
		return expandValue(val, lookup), nil
	}
}

// stripInlineComment removes a trailing # comment from an unquoted value,
// but only if preceded by whitespace.
func stripInlineComment(s string) string {
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && s[i-1] == ' ' {
			return s[:i-1]
		}
	}
	return s
}

// parseDoubleQuoted parses the content after the opening " and returns the value.
func parseDoubleQuoted(s string, lookup func(string) (string, bool)) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		ch := s[i]
		switch ch {
		case '"':
			return b.String(), nil
		case '\\':
			if i+1 >= len(s) {
				return "", fmt.Errorf("unterminated escape in double-quoted string")
			}
			next := s[i+1]
			switch next {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case '$':
				b.WriteByte('$')
			default:
				b.WriteByte('\\')
				b.WriteByte(next)
			}
			i += 2
		case '$':
			name, def, advance := parseVarRef(s[i:])
			if name == "" {
				b.WriteByte('$')
				i++
			} else {
				val, ok := lookup(name)
				if !ok || val == "" {
					val = def
				}
				b.WriteString(val)
				i += advance
			}
		default:
			b.WriteByte(ch)
			i++
		}
	}
	return "", fmt.Errorf("unterminated double quote")
}

// expandValue performs variable substitution on an unquoted value.
func expandValue(s string, lookup func(string) (string, bool)) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '$' {
			name, def, advance := parseVarRef(s[i:])
			if name == "" {
				b.WriteByte('$')
				i++
			} else {
				val, ok := lookup(name)
				if !ok || val == "" {
					val = def
				}
				b.WriteString(val)
				i += advance
			}
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// parseVarRef parses a variable reference starting at '$'.
// Returns (name, default, bytesConsumed). name=="" means no valid reference.
// Supports: $VAR, ${VAR}, ${VAR:-default}
func parseVarRef(s string) (name, def string, advance int) {
	if len(s) < 2 || s[0] != '$' {
		return "", "", 0
	}

	if s[1] == '{' {
		// ${...} form
		end := strings.IndexByte(s, '}')
		if end < 0 {
			return "", "", 0
		}
		inner := s[2:end]
		if colonIdx := strings.Index(inner, ":-"); colonIdx >= 0 {
			return inner[:colonIdx], inner[colonIdx+2:], end + 1
		}
		return inner, "", end + 1
	}

	// $VAR form: consume identifier chars
	i := 1
	for i < len(s) && isVarChar(s[i]) {
		i++
	}
	if i == 1 {
		return "", "", 0
	}
	return s[1:i], "", i
}

func isVarChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func environToMap(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, e := range environ {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		}
	}
	return m
}
