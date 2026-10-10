// Package naming converts identifiers between Go/SQL/URL conventions and handles
// English pluralization for generated code.
package naming

import (
	"strings"
	"unicode"
)

// Words splits "UserProfile", "user_profile", "user-profile", "user profile", "userID"
// into lowercase words: [user profile], [user id].
func Words(s string) []string {
	var words []string
	var cur []rune
	rs := []rune(strings.TrimSpace(s))
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i, r := range rs {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.' || r == '/':
			flush()
		case unicode.IsUpper(r):
			// boundary: aB -> a|B ; ABc -> A|Bc (end of acronym)
			if i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1])) {
				flush()
			} else if i > 0 && unicode.IsUpper(rs[i-1]) && i+1 < len(rs) && unicode.IsLower(rs[i+1]) {
				flush()
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

var initialisms = map[string]string{
	"id": "ID", "url": "URL", "uri": "URI", "api": "API", "http": "HTTP", "json": "JSON", "sql": "SQL",
	"uuid": "UUID", "ip": "IP", "html": "HTML", "sku": "SKU", "otp": "OTP", "jwt": "JWT", "acl": "ACL",
}

// Pascal returns Go exported style with initialisms: "user_id" -> "UserID".
func Pascal(s string) string {
	var b strings.Builder
	for _, w := range Words(s) {
		if up, ok := initialisms[w]; ok {
			b.WriteString(up)
		} else {
			b.WriteString(capitalize(w))
		}
	}
	return b.String()
}

// Camel returns Go unexported style: "user_id" -> "userID", "ID" -> "id".
func Camel(s string) string {
	ws := Words(s)
	if len(ws) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(ws[0])
	for _, w := range ws[1:] {
		if up, ok := initialisms[w]; ok {
			b.WriteString(up)
		} else {
			b.WriteString(capitalize(w))
		}
	}
	return b.String()
}

// JSONName returns lowerCamel without Go initialism upper-casing: "user_id" -> "userId".
func JSONName(s string) string {
	ws := Words(s)
	if len(ws) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(ws[0])
	for _, w := range ws[1:] {
		b.WriteString(capitalize(w))
	}
	return b.String()
}

// Snake returns "user_profile".
func Snake(s string) string { return strings.Join(Words(s), "_") }

// Kebab returns "user-profile".
func Kebab(s string) string { return strings.Join(Words(s), "-") }

// Package returns a valid Go package name: "user-profiles" -> "userprofiles".
func Package(s string) string { return strings.Join(Words(s), "") }

// Human returns "user profile".
func Human(s string) string { return strings.Join(Words(s), " ") }

func capitalize(w string) string {
	if w == "" {
		return w
	}
	r := []rune(w)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

var irregular = map[string]string{
	"person": "people", "child": "children", "man": "men", "woman": "women", "mouse": "mice",
	"goose": "geese", "foot": "feet", "tooth": "teeth", "datum": "data", "criterion": "criteria",
	"status": "statuses", "quiz": "quizzes", "analysis": "analyses",
}

var uncountable = map[string]bool{"equipment": true, "information": true, "news": true, "series": true, "species": true, "metadata": true, "feedback": true, "media": true}

// Plural pluralizes the last word: "course" -> "courses", "category" -> "categories",
// "OrderItem" -> "OrderItems" (case of input preserved for the stem).
func Plural(s string) string {
	ws := Words(s)
	if len(ws) == 0 {
		return s
	}
	last := ws[len(ws)-1]
	p := pluralWord(last)
	return s[:len(s)-len(last)] + matchCase(s[len(s)-len(last):], p)
}

// Singular reverses Plural for common cases.
func Singular(s string) string {
	ws := Words(s)
	if len(ws) == 0 {
		return s
	}
	last := ws[len(ws)-1]
	sg := singularWord(last)
	return s[:len(s)-len(last)] + matchCase(s[len(s)-len(last):], sg)
}

func pluralWord(w string) string {
	if uncountable[w] {
		return w
	}
	if p, ok := irregular[w]; ok {
		return p
	}
	switch {
	case strings.HasSuffix(w, "y") && len(w) > 1 && !strings.ContainsRune("aeiou", rune(w[len(w)-2])):
		return w[:len(w)-1] + "ies"
	case strings.HasSuffix(w, "s"), strings.HasSuffix(w, "x"), strings.HasSuffix(w, "z"), strings.HasSuffix(w, "ch"), strings.HasSuffix(w, "sh"):
		return w + "es"
	case strings.HasSuffix(w, "fe"):
		return w[:len(w)-2] + "ves"
	case strings.HasSuffix(w, "f") && !strings.HasSuffix(w, "ff") && w != "chef" && w != "roof" && w != "proof":
		return w[:len(w)-1] + "ves"
	default:
		return w + "s"
	}
}

func singularWord(w string) string {
	if uncountable[w] {
		return w
	}
	for s, p := range irregular {
		if p == w {
			return s
		}
	}
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 3:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ves"):
		return w[:len(w)-3] + "f"
	case strings.HasSuffix(w, "sses"), strings.HasSuffix(w, "xes"), strings.HasSuffix(w, "zzes"), strings.HasSuffix(w, "ches"), strings.HasSuffix(w, "shes"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "ss"):
		return w
	case strings.HasSuffix(w, "s") && len(w) > 1:
		return w[:len(w)-1]
	default:
		return w
	}
}

// matchCase applies the case pattern of orig ("Course"/"COURSE"/"course") to repl.
func matchCase(orig, repl string) string {
	if orig == "" {
		return repl
	}
	if strings.ToUpper(orig) == orig && len(orig) > 1 {
		return strings.ToUpper(repl)
	}
	if unicode.IsUpper([]rune(orig)[0]) {
		return capitalize(repl)
	}
	return repl
}

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true, "default": true, "defer": true, "else": true,
	"fallthrough": true, "for": true, "func": true, "go": true, "goto": true, "if": true, "import": true, "interface": true,
	"map": true, "package": true, "range": true, "return": true, "select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// IsGoKeyword reports Go reserved words.
func IsGoKeyword(s string) bool { return goKeywords[s] }
