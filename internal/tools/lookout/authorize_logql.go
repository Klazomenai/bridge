package lookout

import (
	"fmt"
)

// AuthorizeLogQL parses a LogQL query and rejects it unless every stream
// selector constrains the "namespace" label to a value (or set of values)
// drawn entirely from the allowlist.
//
// LogQL's stream selector syntax (the `{label="value"}` block at the start of
// a log pipeline, and inside range-vector functions like `rate({...}[5m])`) is
// a strict subset of PromQL's label matcher syntax. We extract every balanced
// `{...}` block from the query, parse each with Prometheus' label matcher
// parser, then apply the same namespace-allowlist rules as AuthorizePromQL.
//
// LogQL has no other use of curly braces at the top level — label_format,
// line_format, json, logfmt, unwrap, and range durations all use different
// delimiters. A regex matcher value can contain braces (e.g. "foo{1,3}"), but
// those live inside a quoted string and are skipped by the string-aware
// extractor.
//
// Rejection cases mirror AuthorizePromQL: missing namespace matcher, negative
// matcher, non-allowlisted equality value, or non-literal regex.
func AuthorizeLogQL(query string, allowlist *NamespaceAllowlist) error {
	if allowlist == nil || allowlist.Len() == 0 {
		return fmt.Errorf("lookout: namespace allowlist is empty — query refused")
	}

	selectors, err := extractStreamSelectors(query)
	if err != nil {
		return fmt.Errorf("lookout: invalid LogQL: %w", err)
	}
	if len(selectors) == 0 {
		return fmt.Errorf("lookout: LogQL query has no stream selector")
	}

	for _, s := range selectors {
		matchers, err := promParser.ParseMetricSelector(s)
		if err != nil {
			return fmt.Errorf("lookout: invalid stream selector %q: %w", s, err)
		}
		if err := checkSelector(matchers, allowlist); err != nil {
			return err
		}
	}
	return nil
}

// extractStreamSelectors walks a LogQL query and returns every top-level
// balanced `{...}` block. String literals (double-quoted, single-quoted, and
// backticked) are skipped so brace-containing regex values inside matchers
// don't confuse the depth counter.
func extractStreamSelectors(query string) ([]string, error) {
	var selectors []string
	runes := []rune(query)
	depth := 0
	start := -1

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Skip quoted strings so `{` and `}` inside regex values don't unbalance
		// the counter. LogQL accepts three string delimiters.
		switch r {
		case '"', '\'':
			j, err := skipQuoted(runes, i, r)
			if err != nil {
				return nil, err
			}
			i = j
			continue
		case '`':
			j, err := skipRaw(runes, i)
			if err != nil {
				return nil, err
			}
			i = j
			continue
		}

		switch r {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced '}' at position %d", i)
			}
			if depth == 0 && start >= 0 {
				selectors = append(selectors, string(runes[start:i+1]))
				start = -1
			}
		}
	}

	if depth != 0 {
		return nil, fmt.Errorf("unterminated '{' (depth %d at end of query)", depth)
	}
	return selectors, nil
}

// skipQuoted advances past a double- or single-quoted string literal (with
// backslash escapes), returning the index of the closing quote.
func skipQuoted(runes []rune, start int, quote rune) (int, error) {
	for i := start + 1; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			// Skip the escaped char (if any).
			if i+1 < len(runes) {
				i++
			}
		case quote:
			return i, nil
		}
	}
	return 0, fmt.Errorf("unterminated %q string literal starting at position %d", quote, start)
}

// skipRaw advances past a backtick-delimited raw string literal, returning
// the index of the closing backtick. Raw strings do not honour backslash
// escapes.
func skipRaw(runes []rune, start int) (int, error) {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == '`' {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unterminated raw string literal starting at position %d", start)
}
