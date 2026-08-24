package planner

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	quotedPattern     = regexp.MustCompile("(?:\\\"([^\\\"]+)\\\"|'([^']+)'|`([^`]+)`)")
	identifierPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_:.\/\[\]-]*`)
)

var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"buat": true, "because": true, "by": true, "di": true, "dengan": true,
	"find": true, "fix": true, "for": true, "from": true, "in": true, "into": true, "is": true,
	"ke": true, "of": true, "on": true, "review": true, "the": true, "this": true,
	"locate": true, "to": true, "ubah": true, "untuk": true, "where": true, "who": true, "yang": true, "dan": true,
}

func interpretTask(task string) TaskIntent {
	intent := TaskIntent{RawTask: task, TaskClass: "broad_unknown", Confidence: "low", ExpansionDepth: 1}
	quoted := quotedPattern.FindAllStringSubmatch(task, -1)
	for _, match := range quoted {
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				intent.QuotedIdentifiers = appendTaskUnique(intent.QuotedIdentifiers, match[i])
				intent.Terms = appendTaskUnique(intent.Terms, match[i])
				break
			}
		}
	}
	for _, token := range identifierPattern.FindAllString(task, -1) {
		lower := strings.ToLower(token)
		if stopwords[lower] || len(token) < 3 {
			continue
		}
		if strings.ContainsAny(token, ".:/[]-_") || hasCodeCase(token) || !isPlainWord(token) {
			intent.Terms = appendTaskUnique(intent.Terms, token)
			continue
		}
		// Plain words remain lookup hints only when they are not common prose.
		intent.Terms = appendTaskUnique(intent.Terms, token)
	}
	if len(intent.Terms) > 32 {
		intent.Terms = intent.Terms[:32]
	}
	for _, term := range intent.Terms {
		if looksLikeFile(term) {
			intent.FileHints = appendTaskUnique(intent.FileHints, term)
		}
		if looksLikeOperation(term) {
			intent.FrameworkOperations = appendTaskUnique(intent.FrameworkOperations, term)
		}
		// After stopword removal, every remaining term is a bounded exact
		// lookup hint. This preserves lowercase identifiers such as save while
		// refusing to interpret common prose as a symbol.
		intent.SymbolHints = appendTaskUnique(intent.SymbolHints, term)
		intent.SemanticHints = appendTaskUnique(intent.SemanticHints, term)
	}
	if len(intent.QuotedIdentifiers) > 0 {
		intent.SymbolHints = appendUniqueAll(intent.SymbolHints, intent.QuotedIdentifiers...)
		intent.SemanticHints = appendUniqueAll(intent.SemanticHints, intent.QuotedIdentifiers...)
	}
	lower := strings.ToLower(task)
	switch {
	case strings.Contains(lower, "caller") || strings.Contains(lower, "who calls"):
		intent.TaskClass, intent.Confidence, intent.TraceDirection = "relationship_trace", "medium", "incoming"
	case strings.Contains(lower, "what does") || strings.Contains(lower, "callees") || strings.Contains(lower, "calls"):
		intent.TaskClass, intent.Confidence, intent.TraceDirection = "relationship_trace", "medium", "outgoing"
	case strings.Contains(lower, "trace") || strings.Contains(lower, "flow") || strings.Contains(lower, "relationship"):
		intent.TaskClass, intent.Confidence, intent.TraceDirection, intent.ExpansionDepth = "relationship_trace", "medium", "both", 2
	case strings.Contains(lower, "cross-resource") || strings.Contains(lower, "cross resource") || strings.Contains(lower, "between"):
		intent.TaskClass, intent.Confidence, intent.TraceDirection = "cross_resource", "medium", "both"
	case strings.Contains(lower, "fix") || strings.Contains(lower, "change") || strings.Contains(lower, "modify") || strings.Contains(lower, "implement") || strings.Contains(lower, "refactor") || strings.Contains(lower, "perbaiki") || strings.Contains(lower, "ubah"):
		intent.TaskClass, intent.Confidence, intent.TraceDirection = "localized_change", "medium", "both"
	case strings.Contains(lower, "find") || strings.Contains(lower, "locate") || strings.Contains(lower, "where is") || strings.Contains(lower, "cari"):
		intent.TaskClass, intent.Confidence = "exact_symbol", "medium"
	}
	return intent
}

func adjustTaskClass(intent *TaskIntent, exactSymbols, exactSemantic int) {
	if exactSymbols == 0 && exactSemantic == 0 {
		if intent.TaskClass != "relationship_trace" && intent.TaskClass != "cross_resource" {
			intent.TaskClass, intent.Confidence = "broad_unknown", "low"
		}
		return
	}
	if exactSymbols == 1 && exactSemantic == 0 && intent.TaskClass == "broad_unknown" {
		intent.TaskClass, intent.Confidence = "exact_symbol", "high"
	} else if exactSemantic == 1 && exactSymbols == 0 && intent.TaskClass == "broad_unknown" {
		intent.TaskClass, intent.Confidence = "exact_semantic", "high"
	} else if exactSymbols > 1 || exactSemantic > 1 {
		if intent.Confidence == "high" {
			intent.Confidence = "medium"
		}
	}
}

func hasCodeCase(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func isPlainWord(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func looksLikeFile(s string) bool {
	return strings.Contains(s, "/") || strings.Contains(s, "\\") || strings.HasSuffix(strings.ToLower(s), ".lua") || strings.HasSuffix(strings.ToLower(s), ".go") || strings.HasSuffix(strings.ToLower(s), ".ts") || strings.HasSuffix(strings.ToLower(s), ".tsx") || strings.HasSuffix(strings.ToLower(s), ".js") || strings.HasSuffix(strings.ToLower(s), ".jsx")
}
func looksLikeOperation(s string) bool { return strings.Contains(s, "_") && !strings.HasPrefix(s, "_") }
func looksLikeSymbol(s string) bool {
	return hasCodeCase(s) || strings.ContainsAny(s, "._:") || !isPlainWord(s)
}

func appendTaskUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueAll(values []string, additions ...string) []string {
	for _, value := range additions {
		values = appendTaskUnique(values, value)
	}
	sort.Strings(values)
	return values
}
