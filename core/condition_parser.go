package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ParseConditionString parses a compact condition expression into a ConditionConfig.
func ParseConditionString(s string) (ConditionConfig, error) {
	tokens, err := tokenize(s)
	if err != nil {
		return ConditionConfig{}, fmt.Errorf("condition parse error: %w", err)
	}
	if len(tokens) < 3 {
		return ConditionConfig{}, fmt.Errorf("condition parse error: expected '<field> <operator> <value>', got %q", s)
	}

	field := tokens[0]
	op := tokens[1]
	rawValue := strings.Join(tokens[2:], " ")

	if field == "" {
		return ConditionConfig{}, fmt.Errorf("condition parse error: field name is empty in %q", s)
	}

	if !isValidOperator(op) {
		return ConditionConfig{}, fmt.Errorf("condition parse error: unknown operator %q in %q (valid: eq ne == != > >= < <= contains matches)", op, s)
	}

	var value interface{}
	switch op {
	case "matches":
		if !strings.HasPrefix(rawValue, "/") || !strings.HasSuffix(rawValue, "/") || len(rawValue) < 2 {
			return ConditionConfig{}, fmt.Errorf("condition parse error: matches operator requires a /regexp/ value, got %q", rawValue)
		}
		pattern := rawValue[1 : len(rawValue)-1]
		if _, err := regexp.Compile(pattern); err != nil {
			return ConditionConfig{}, fmt.Errorf("condition parse error: invalid regexp %q: %w", pattern, err)
		}
		value = rawValue
	case "==", "!=", ">", ">=", "<", "<=":
		f, err := strconv.ParseFloat(rawValue, 64)
		if err != nil {
			return ConditionConfig{}, fmt.Errorf("condition parse error: operator %q requires a numeric value, got %q", op, rawValue)
		}
		value = f
	default:
		value = rawValue
	}

	return ConditionConfig{Field: field, Operator: op, Value: value}, nil
}

// isValidOperator returns true for all recognised condition operators.
func isValidOperator(op string) bool {
	switch op {
	case "eq", "ne", "==", "!=", ">", ">=", "<", "<=", "contains", "matches":
		return true
	}
	return false
}

// tokenize splits a condition string into tokens respecting quoted strings and regexp literals.
func tokenize(s string) ([]string, error) {
	var tokens []string
	r := []rune(s)
	i := 0
	n := len(r)

	for i < n {
		for i < n && unicode.IsSpace(r[i]) {
			i++
		}
		if i >= n {
			break
		}

		switch r[i] {
		case '"', '\'':
			quote := r[i]
			i++
			var buf strings.Builder
			for i < n {
				if r[i] == '\\' && i+1 < n && r[i+1] == quote {
					buf.WriteRune(quote)
					i += 2
					continue
				}
				if r[i] == quote {
					i++
					break
				}
				buf.WriteRune(r[i])
				i++
			}
			tokens = append(tokens, buf.String())

		case '/':
			i++
			var buf strings.Builder
			buf.WriteRune('/')
			for i < n {
				if r[i] == '\\' && i+1 < n && r[i+1] == '/' {
					buf.WriteRune('/')
					i += 2
					continue
				}
				if r[i] == '/' {
					buf.WriteRune('/')
					i++
					break
				}
				buf.WriteRune(r[i])
				i++
			}
			tokens = append(tokens, buf.String())

		default:
			var buf strings.Builder
			for i < n && !unicode.IsSpace(r[i]) {
				buf.WriteRune(r[i])
				i++
			}
			tokens = append(tokens, buf.String())
		}
	}

	return tokens, nil
}
