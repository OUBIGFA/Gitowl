package editor

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

type lexLang uint8

const (
	langPlain lexLang = iota
	langGeneric
	langGo
	langRust
	langCStyle
	langJS
	langPython
	langRuby
	langPHP
	langShell
	langJSON
	langJSONC
	langYAML
	langTOML
	langSQL
	langHTML
	langCSS
	langMarkdown
	langMermaid
)

type lexState uint8

const (
	lexNormal lexState = iota
	lexBlockComment
	lexBacktick
	lexTripleDouble
	lexTripleSingle
	lexHTMLComment
)

type lexedLine struct {
	raw      string
	inState  lexState
	outState lexState
	spans    []ui.Span
}

type codeLexCache struct {
	lang     lexLang
	baseText ui.Color
	tokens   CodeTokens
	lines    []lexedLine
}

// Code spans change only paint. Their text and font metrics match the buffer.
func codeSpans(text string, pg Page) []ui.Span {
	return codeSpansFor("", "go", text, pg, nil)
}

func codeSpansFor(id, language, text string, pg Page, cache map[string]*codeLexCache) []ui.Span {
	if text == "" {
		return []ui.Span{{Text: "", Color: pg.CodeText}}
	}
	lang := normalizeLexLang(language)
	if lang == langPlain {
		return []ui.Span{{Text: text, Color: pg.CodeText}}
	}

	var prev *codeLexCache
	if cache != nil && id != "" {
		if c := cache[id]; c != nil && c.lang == lang && c.baseText == pg.CodeText && c.tokens == pg.Tokens {
			prev = c
		}
	}

	lineCount := 1 + strings.Count(text, "\n")
	lines := make([]lexedLine, 0, lineCount)
	totalSpans := 0
	state := lexNormal
	pos := 0
	for lineIdx := 0; pos < len(text) || lineIdx == 0; lineIdx++ {
		nl := strings.IndexByte(text[pos:], '\n')
		var line string
		if nl < 0 {
			line = text[pos:]
			pos = len(text)
		} else {
			line = text[pos : pos+nl+1]
			pos += nl + 1
		}
		if prev != nil && lineIdx < len(prev.lines) && prev.lines[lineIdx].raw == line && prev.lines[lineIdx].inState == state {
			cached := prev.lines[lineIdx]
			lines = append(lines, cached)
			state = cached.outState
			totalSpans += len(cached.spans)
		} else {
			spans, nextState := lexLine(lang, line, state, pg)
			lines = append(lines, lexedLine{raw: line, inState: state, outState: nextState, spans: spans})
			state = nextState
			totalSpans += len(spans)
		}
		if pos >= len(text) {
			break
		}
	}

	if cache != nil && id != "" {
		cache[id] = &codeLexCache{lang: lang, baseText: pg.CodeText, tokens: pg.Tokens, lines: lines}
	}

	out := make([]ui.Span, 0, totalSpans)
	for _, ln := range lines {
		for _, sp := range ln.spans {
			if sp.Text == "" {
				continue
			}
			if len(out) > 0 && out[len(out)-1].Color == sp.Color {
				out[len(out)-1].Text += sp.Text
			} else {
				out = append(out, sp)
			}
		}
	}
	if len(out) == 0 {
		return []ui.Span{{Text: text, Color: pg.CodeText}}
	}
	return out
}

func normalizeLexLang(language string) lexLang {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "plain", "plaintext", "text", "txt":
		return langPlain
	case "go", "golang":
		return langGo
	case "rust", "rs":
		return langRust
	case "c", "cpp", "c++", "cc", "cxx", "h", "hpp", "java", "csharp", "cs", "swift", "kotlin", "kt":
		return langCStyle
	case "javascript", "js", "jsx", "mjs", "cjs", "typescript", "ts", "tsx":
		return langJS
	case "python", "py":
		return langPython
	case "ruby", "rb":
		return langRuby
	case "php":
		return langPHP
	case "bash", "sh", "zsh", "shell", "dockerfile":
		return langShell
	case "json":
		return langJSON
	case "jsonc":
		return langJSONC
	case "yaml", "yml":
		return langYAML
	case "toml":
		return langTOML
	case "sql":
		return langSQL
	case "html", "xml", "svg":
		return langHTML
	case "css", "scss", "less":
		return langCSS
	case "markdown", "md":
		return langMarkdown
	case "mermaid":
		return langMermaid
	default:
		return langGeneric
	}
}

func lexLine(lang lexLang, line string, state lexState, pg Page) ([]ui.Span, lexState) {
	var spans []ui.Span
	emit := func(text string, color ui.Color) {
		if text == "" {
			return
		}
		if len(spans) > 0 && spans[len(spans)-1].Color == color {
			spans[len(spans)-1].Text += text
		} else {
			spans = append(spans, ui.Span{Text: text, Color: color})
		}
	}

	switch lang {
	case langMarkdown:
		return lexMarkdownLine(line, pg), lexNormal
	case langMermaid:
		return lexMermaidLine(line, pg), lexNormal
	case langHTML:
		return lexHTMLLine(line, state, pg)
	case langYAML, langTOML:
		return lexConfigLine(lang, line, pg), lexNormal
	}

	n := len(line)
	i := 0

	// Resume multi-line states at line start.
	switch state {
	case lexBlockComment:
		if end := strings.Index(line, "*/"); end >= 0 {
			emit(line[:end+2], pg.Tokens.Comment)
			i = end + 2
			state = lexNormal
		} else {
			emit(line, pg.Tokens.Comment)
			return spans, lexBlockComment
		}
	case lexBacktick:
		end := scanStringUnescaped(line, '`')
		if end >= 0 {
			emit(line[:end+1], pg.Tokens.String)
			i = end + 1
			state = lexNormal
		} else {
			emit(line, pg.Tokens.String)
			return spans, lexBacktick
		}
	case lexTripleDouble:
		end := scanTripleQuote(line, `"""`)
		if end >= 0 {
			emit(line[:end+3], pg.Tokens.String)
			i = end + 3
			state = lexNormal
		} else {
			emit(line, pg.Tokens.String)
			return spans, lexTripleDouble
		}
	case lexTripleSingle:
		end := scanTripleQuote(line, `'''`)
		if end >= 0 {
			emit(line[:end+3], pg.Tokens.String)
			i = end + 3
			state = lexNormal
		} else {
			emit(line, pg.Tokens.String)
			return spans, lexTripleSingle
		}
	}

	// C/C++/C# preprocessor directive lines (#include, #define, etc.).
	if i == 0 && lang == langCStyle {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			ws := len(line) - len(trimmed)
			emit(line[:ws], pg.CodeText)
			// Color directive word as Keyword, keep inline comments if present.
			j := ws + 1
			for j < n && isIdentCont(line[j]) {
				j++
			}
			emit(line[ws:j], pg.Tokens.Keyword)
			i = j
		}
	}

	for i < n {
		c := line[i]

		// Line comments.
		if hasSlashSlashComment(lang) && i+1 < n && c == '/' && line[i+1] == '/' {
			emit(line[i:], pg.Tokens.Comment)
			return spans, lexNormal
		}
		if lang == langSQL && i+1 < n && c == '-' && line[i+1] == '-' {
			emit(line[i:], pg.Tokens.Comment)
			return spans, lexNormal
		}
		if hasHashComment(lang) && c == '#' {
			emit(line[i:], pg.Tokens.Comment)
			return spans, lexNormal
		}

		// Block comments /* ... */.
		if hasBlockComment(lang) && i+1 < n && c == '/' && line[i+1] == '*' {
			if end := strings.Index(line[i+2:], "*/"); end >= 0 {
				emit(line[i:i+2+end+2], pg.Tokens.Comment)
				i += 2 + end + 2
				continue
			}
			emit(line[i:], pg.Tokens.Comment)
			return spans, lexBlockComment
		}

		// Rust attributes (#[...] / #![...]).
		if lang == langRust && c == '#' && (i+1 < n && (line[i+1] == '[' || (line[i+1] == '!' && i+2 < n && line[i+2] == '['))) {
			if closeIdx := strings.IndexByte(line[i:], ']'); closeIdx >= 0 {
				emit(line[i:i+closeIdx+1], pg.Tokens.Keyword)
				i += closeIdx + 1
				continue
			}
			emit(line[i:], pg.Tokens.Keyword)
			return spans, lexNormal
		}

		// CSS hex colors (#RGB, #RRGGBB, #RRGGBBAA).
		if lang == langCSS && c == '#' && i+1 < n && isHexDigit(line[i+1]) {
			j := i + 1
			for j < n && isHexDigit(line[j]) {
				j++
			}
			emit(line[i:j], pg.Tokens.Number)
			i = j
			continue
		}

		// CSS @rules or Java/Python/Swift/Kotlin @Annotations.
		if c == '@' && (lang == langCSS || lang == langPython || lang == langCStyle) && i+1 < n && isIdentStart(line[i+1]) {
			j := i + 1
			for j < n && (isIdentCont(line[j]) || line[j] == '-') {
				j++
			}
			col := pg.Tokens.Function
			if lang == langCSS {
				col = pg.Tokens.Keyword
			}
			emit(line[i:j], col)
			i = j
			continue
		}

		// Shell / PHP variables ($VAR, ${VAR}).
		if (lang == langShell || lang == langPHP) && c == '$' && i+1 < n {
			if line[i+1] == '{' {
				if closeIdx := strings.IndexByte(line[i+2:], '}'); closeIdx >= 0 {
					emit(line[i:i+2+closeIdx+1], pg.Tokens.Type)
					i += 2 + closeIdx + 1
					continue
				}
			} else if isIdentStart(line[i+1]) {
				j := i + 1
				for j < n && isIdentCont(line[j]) {
					j++
				}
				emit(line[i:j], pg.Tokens.Type)
				i = j
				continue
			}
		}

		// Python triple-quoted strings and raw/f-string prefixes.
		if lang == langPython {
			if i+2 < n && (line[i:i+3] == `"""` || line[i:i+3] == `'''`) {
				delim := line[i : i+3]
				if end := scanTripleQuote(line[i+3:], delim); end >= 0 {
					emit(line[i:i+3+end+3], pg.Tokens.String)
					i += 3 + end + 3
					continue
				}
				emit(line[i:], pg.Tokens.String)
				if delim == `"""` {
					return spans, lexTripleDouble
				}
				return spans, lexTripleSingle
			}
		}

		// Rust raw strings r#"..."# / r"..." / b"...".
		if lang == langRust && (c == 'r' || c == 'b') && i+1 < n && (line[i+1] == '"' || line[i+1] == '#') {
			if end, ok := scanRustRawString(line[i:]); ok {
				emit(line[i:i+end], pg.Tokens.String)
				i += end
				continue
			}
		}

		// Rust lifetimes ('a, 'static) vs character literals ('x', '\n').
		if lang == langRust && c == '\'' {
			if i+1 < n && isIdentStart(line[i+1]) && !(i+2 < n && line[i+2] == '\'') {
				j := i + 1
				for j < n && isIdentCont(line[j]) {
					j++
				}
				if j < n && line[j] == '\'' {
					emit(line[i:j+1], pg.Tokens.String)
					i = j + 1
				} else {
					emit(line[i:j], pg.Tokens.Type)
					i = j
				}
				continue
			}
		}

		// Backtick raw/template strings in Go and JS/TS.
		if c == '`' && (lang == langGo || lang == langJS) {
			if end := scanStringUnescaped(line[i+1:], '`'); end >= 0 {
				emit(line[i:i+1+end+1], pg.Tokens.String)
				i += 1 + end + 1
				continue
			}
			emit(line[i:], pg.Tokens.String)
			return spans, lexBacktick
		}

		// Quoted strings "..." or '...'.
		if c == '"' || (c == '\'' && lang != langJSON && lang != langJSONC) {
			quote := c
			j := i + 1
			for j < n && line[j] != '\n' {
				if line[j] == '\\' && j+1 < n {
					j += 2
					continue
				}
				if line[j] == quote {
					j++
					break
				}
				j++
			}
			col := pg.Tokens.String
			if (lang == langJSON || lang == langJSONC) && quote == '"' {
				rest := strings.TrimLeft(line[j:], " \t")
				if strings.HasPrefix(rest, ":") {
					col = pg.Tokens.Type
				}
			}
			emit(line[i:j], col)
			i = j
			continue
		}

		// Numbers (hex, binary, decimal, float, CSS units).
		if c >= '0' && c <= '9' {
			j := i + 1
			for j < n && (isIdentCont(line[j]) || (line[j] == '.' && j+1 < n && line[j+1] >= '0' && line[j+1] <= '9')) {
				j++
			}
			if lang == langCSS && j < n && line[j] == '%' {
				j++
			}
			emit(line[i:j], pg.Tokens.Number)
			i = j
			continue
		}

		// Identifiers / Keywords / Types / Function calls.
		if isIdentStart(c) {
			j := i + 1
			for j < n && (isIdentCont(line[j]) || (lang == langCSS && line[j] == '-')) {
				j++
			}
			tok := line[i:j]
			// Check CSS property: name.
			if lang == langCSS {
				rest := strings.TrimLeft(line[j:], " \t")
				if strings.HasPrefix(rest, ":") {
					emit(tok, pg.Tokens.Type)
					i = j
					continue
				}
			}
			// Check Python string prefixes f"...", r"...", b"...".
			if lang == langPython && j < n && (line[j] == '"' || line[j] == '\'') && isPythonStringPrefix(tok) {
				emit(tok, pg.Tokens.Keyword)
				i = j
				continue
			}
			col := classifyToken(lang, tok, line[j:], pg)
			emit(tok, col)
			i = j
			continue
		}

		// Operators, whitespace, and punctuation.
		j := i + 1
		for j < n && !isTokenBoundary(lang, line[j]) {
			j++
		}
		emit(line[i:j], pg.CodeText)
		i = j
	}

	return spans, state
}

func isTokenBoundary(lang lexLang, b byte) bool {
	if isIdentStart(b) || (b >= '0' && b <= '9') || b == '"' || b == '\'' || b == '`' || b == '/' || b == '#' || b == '@' || b == '$' {
		return true
	}
	if lang == langSQL && b == '-' {
		return true
	}
	return false
}

func hasSlashSlashComment(lang lexLang) bool {
	switch lang {
	case langGo, langRust, langCStyle, langJS, langPHP, langJSONC, langGeneric:
		return true
	}
	return false
}

func hasHashComment(lang lexLang) bool {
	switch lang {
	case langPython, langRuby, langPHP, langShell, langGeneric:
		return true
	}
	return false
}

func hasBlockComment(lang lexLang) bool {
	switch lang {
	case langGo, langRust, langCStyle, langJS, langPHP, langJSONC, langSQL, langCSS, langGeneric:
		return true
	}
	return false
}

func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isIdentCont(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func isPythonStringPrefix(s string) bool {
	switch strings.ToLower(s) {
	case "f", "r", "b", "u", "fr", "rf", "br", "rb":
		return true
	}
	return false
}

func scanStringUnescaped(s string, quote byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == quote {
			return i
		}
	}
	return -1
}

func scanTripleQuote(s, delim string) int {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			continue
		}
		if s[i:i+3] == delim {
			return i
		}
	}
	return -1
}

func scanRustRawString(s string) (int, bool) {
	i := 0
	if s[i] == 'b' {
		i++
	}
	if i >= len(s) || s[i] != 'r' {
		return 0, false
	}
	i++
	hashes := 0
	for i < len(s) && s[i] == '#' {
		hashes++
		i++
	}
	if i >= len(s) || s[i] != '"' {
		return 0, false
	}
	i++
	for i < len(s) {
		if s[i] == '"' {
			ok := true
			if i+1+hashes <= len(s) {
				for h := 0; h < hashes; h++ {
					if s[i+1+h] != '#' {
						ok = false
						break
					}
				}
				if ok {
					return i + 1 + hashes, true
				}
			}
		}
		i++
	}
	return 0, false
}

func classifyToken(lang lexLang, tok, rest string, pg Page) ui.Color {
	switch lang {
	case langJSON, langJSONC:
		if tok == "true" || tok == "false" || tok == "null" {
			return pg.Tokens.Keyword
		}
		return pg.CodeText
	case langCSS:
		if len(rest) > 0 && rest[0] == '(' {
			return pg.Tokens.Function
		}
		return pg.CodeText
	case langSQL:
		if sqlKeywords[strings.ToUpper(tok)] {
			return pg.Tokens.Keyword
		}
		if len(rest) > 0 && rest[0] == '(' {
			return pg.Tokens.Function
		}
		return pg.CodeText
	}
	if keywordForLang(lang, tok) {
		return pg.Tokens.Keyword
	}
	if builtinTypeForLang(lang, tok) {
		return pg.Tokens.Type
	}
	if len(tok) > 0 && tok[0] >= 'A' && tok[0] <= 'Z' && !isAllUpper(tok) {
		return pg.Tokens.Type
	}
	if len(rest) > 0 && (rest[0] == '(' || (lang == langRust && strings.HasPrefix(rest, "!"))) {
		return pg.Tokens.Function
	}
	return pg.CodeText
}

func isAllUpper(s string) bool {
	hasLetter := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' {
			return false
		}
		if s[i] >= 'A' && s[i] <= 'Z' {
			hasLetter = true
		}
	}
	return hasLetter
}

func keywordForLang(lang lexLang, tok string) bool {
	switch lang {
	case langGo:
		return goKeywords[tok]
	case langRust:
		return rustKeywords[tok]
	case langCStyle:
		return cStyleKeywords[tok]
	case langJS:
		return jsKeywords[tok]
	case langPython:
		return pythonKeywords[tok]
	case langRuby:
		return rubyKeywords[tok]
	case langPHP:
		return phpKeywords[tok]
	case langShell:
		return shellKeywords[tok] || shellKeywords[strings.ToLower(tok)]
	default:
		return genericKeywords[tok]
	}
}

func builtinTypeForLang(lang lexLang, tok string) bool {
	switch lang {
	case langGo:
		return goTypes[tok]
	case langRust:
		return rustTypes[tok]
	case langCStyle:
		return cStyleTypes[tok]
	case langJS:
		return jsTypes[tok]
	case langPython:
		return pythonTypes[tok]
	}
	return false
}

func wordSet(words string) map[string]bool {
	fields := strings.Fields(words)
	m := make(map[string]bool, len(fields))
	for _, w := range fields {
		m[w] = true
	}
	return m
}

var (
	goKeywords = wordSet("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var true false nil iota")
	goTypes    = wordSet("any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr")

	rustKeywords = wordSet("as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while")
	rustTypes    = wordSet("bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize String Vec Option Result Box Arc Rc")

	cStyleKeywords = wordSet("abstract as async await break case catch class const continue default defer delegate do dynamic else enum export extends final finally for foreach from func function get goto guard if implements import in init inline interface internal is let namespace new null override package private protected public return sealed set sizeof static struct super switch template this throw throws true false try type typedef typename using var virtual void volatile where while yield nil")
	cStyleTypes    = wordSet("auto bool byte char double float int int8_t int16_t int32_t int64_t long short signed size_t string uint8_t uint16_t uint32_t uint64_t unsigned Int Double Float Bool String List Map Set Array")

	jsKeywords = wordSet("as async await break case catch class const continue debugger default declare delete do else enum export extends false finally for from function get if implements import in instanceof interface keyof let namespace new null of package private protected public readonly return set static super switch this throw true try type typeof undefined var void while with yield")
	jsTypes    = wordSet("any bigint boolean never number object string symbol unknown Array Promise Record Map Set Date Error RegExp")

	pythonKeywords = wordSet("and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield match case")
	pythonTypes    = wordSet("bool bytes dict float int list object set str tuple type Exception")

	rubyKeywords  = wordSet("BEGIN END alias and begin break case class def defined? do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield")
	phpKeywords   = wordSet("abstract and array as break callable case catch class clone const continue declare default do echo else elseif empty enddeclare endfor endforeach endif endswitch endwhile enum eval exit extends final finally fn for foreach function global goto if implements include include_once instanceof insteadof interface isset list match namespace new or print private protected public readonly require require_once return static switch throw trait try unset use var while xor yield true false null")
	shellKeywords = wordSet("if then else elif fi case esac for while until do done in function select time coproc export local readonly declare unset return exit break continue trap source alias FROM RUN CMD LABEL MAINTAINER EXPOSE ENV ADD COPY ENTRYPOINT VOLUME USER WORKDIR ARG ONBUILD STOPSIGNAL HEALTHCHECK SHELL true false")

	sqlKeywords = wordSet("ADD ALL ALTER AND ANY AS ASC AUTHORIZATION BACKUP BEGIN BETWEEN BREAK BROWSE BULK BY CASCADE CASE CHECK CHECKPOINT CLOSE CLUSTERED COALESCE COLLATE COLUMN COMMIT COMPUTE CONSTRAINT CONTAINS CONTINUE CONVERT CREATE CROSS CURRENT CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP CURRENT_USER CURSOR DATABASE DBCC DEALLOCATE DECLARE DEFAULT DELETE DENY DESC DISK DISTINCT DISTRIBUTED DOUBLE DROP DUMP ELSE END ERRLVL ESCAPE EXCEPT EXEC EXECUTE EXISTS EXIT EXTERNAL FETCH FILE FILLFACTOR FOR FOREIGN FREETEXT FROM FULL FUNCTION GOTO GRANT GROUP HAVING HOLDLOCK IDENTITY IF IN INDEX INNER INSERT INTERSECT INTO IS JOIN KEY KILL LEFT LIKE LIMIT LINENO LOAD MERGE NATIONAL NOCHECK NONCLUSTERED NOT NULL NULLIF OF OFF OFFSETS ON OPEN OPENDATASOURCE OPENQUERY OPENROWSET OPENXML OPTION OR ORDER OUTER OVER PERCENT PIVOT PLAN PRECISION PRIMARY PRINT PROC PROCEDURE PUBLIC RAISERROR READ READTEXT RECONFIGURE REFERENCES REPLICATION RESTORE RESTRICT RETURN REVERT REVOKE RIGHT ROLLBACK ROWCOUNT ROWGUIDCOL RULE SAVE SCHEMA SECURITYAUDIT SELECT SEMANTICKEYPHRASETABLE SEMANTICSIMILARITYDETAILSTABLE SEMANTICSIMILARITYTABLE SESSION_USER SET SETUSER SHUTDOWN SOME STATISTICS SYSTEM_USER TABLE TABLESAMPLE TEXTSIZE THEN TO TOP TRAN TRANSACTION TRIGGER TRUNCATE TRY_CONVERT UNION UNIQUE UNPIVOT UPDATE UPDATETEXT USE USER VALUES VARYING VIEW WAITFOR WHEN WHERE WHILE WITH WITHIN WRITETEXT TRUE FALSE")

	genericKeywords = wordSet("func function return if else for while range break continue switch case default type struct interface var const let import from package def class public private static fn pub impl enum use match true false nil null None True False async await try catch throw new select go defer")
)

func lexHTMLLine(line string, state lexState, pg Page) ([]ui.Span, lexState) {
	var spans []ui.Span
	emit := func(text string, color ui.Color) {
		if text == "" {
			return
		}
		if len(spans) > 0 && spans[len(spans)-1].Color == color {
			spans[len(spans)-1].Text += text
		} else {
			spans = append(spans, ui.Span{Text: text, Color: color})
		}
	}
	n := len(line)
	i := 0
	if state == lexHTMLComment {
		if end := strings.Index(line, "-->"); end >= 0 {
			emit(line[:end+3], pg.Tokens.Comment)
			i = end + 3
			state = lexNormal
		} else {
			emit(line, pg.Tokens.Comment)
			return spans, lexHTMLComment
		}
	}
	for i < n {
		if strings.HasPrefix(line[i:], "<!--") {
			if end := strings.Index(line[i+4:], "-->"); end >= 0 {
				emit(line[i:i+4+end+3], pg.Tokens.Comment)
				i += 4 + end + 3
				continue
			}
			emit(line[i:], pg.Tokens.Comment)
			return spans, lexHTMLComment
		}
		if line[i] == '<' && i+1 < n && (isIdentStart(line[i+1]) || line[i+1] == '/' || line[i+1] == '!') {
			j := i + 1
			if j < n && (line[j] == '/' || line[j] == '!') {
				j++
			}
			for j < n && (isIdentCont(line[j]) || line[j] == '-' || line[j] == ':') {
				j++
			}
			emit(line[i:j], pg.Tokens.Keyword)
			i = j
			for i < n && line[i] != '>' {
				c := line[i]
				if c == '"' || c == '\'' {
					k := i + 1
					for k < n && line[k] != '\n' && line[k] != c {
						k++
					}
					if k < n && line[k] == c {
						k++
					}
					emit(line[i:k], pg.Tokens.String)
					i = k
					continue
				}
				if isIdentStart(c) {
					k := i + 1
					for k < n && (isIdentCont(line[k]) || line[k] == '-' || line[k] == ':') {
						k++
					}
					emit(line[i:k], pg.Tokens.Type)
					i = k
					continue
				}
				emit(line[i:i+1], pg.CodeText)
				i++
			}
			if i < n && line[i] == '>' {
				emit(">", pg.Tokens.Keyword)
				i++
			}
			continue
		}
		next := strings.IndexByte(line[i:], '<')
		if next < 0 {
			emit(line[i:], pg.CodeText)
			break
		}
		emit(line[i:i+next], pg.CodeText)
		i += next
	}
	return spans, state
}

func lexConfigLine(lang lexLang, line string, pg Page) []ui.Span {
	var spans []ui.Span
	emit := func(text string, color ui.Color) {
		if text == "" {
			return
		}
		if len(spans) > 0 && spans[len(spans)-1].Color == color {
			spans[len(spans)-1].Text += text
		} else {
			spans = append(spans, ui.Span{Text: text, Color: color})
		}
	}
	n := len(line)
	i := 0
	for i < n && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i > 0 {
		emit(line[:i], pg.CodeText)
	}
	if i >= n {
		return spans
	}
	if line[i] == '#' {
		emit(line[i:], pg.Tokens.Comment)
		return spans
	}
	if lang == langTOML && line[i] == '[' {
		if end := strings.LastIndexByte(line[i:], ']'); end >= 0 {
			emit(line[i:i+end+1], pg.Tokens.Function)
			i += end + 1
		}
	}
	if lang == langYAML && strings.HasPrefix(line[i:], "- ") {
		emit("- ", pg.Tokens.Keyword)
		i += 2
	}
	// Highlight key before ':' (YAML) or '=' (TOML).
	sep := byte(':')
	if lang == langTOML {
		sep = '='
	}
	if idx := strings.IndexByte(line[i:], sep); idx > 0 {
		keyPart := line[i : i+idx]
		if !strings.ContainsAny(keyPart, "#\"'") {
			emit(keyPart, pg.Tokens.Type)
			emit(line[i+idx:i+idx+1], pg.CodeText)
			i += idx + 1
		}
	}
	for i < n {
		c := line[i]
		if c == '#' {
			emit(line[i:], pg.Tokens.Comment)
			break
		}
		if c == '"' || c == '\'' {
			j := i + 1
			for j < n && line[j] != '\n' {
				if line[j] == '\\' && c == '"' && j+1 < n {
					j += 2
					continue
				}
				if line[j] == c {
					j++
					break
				}
				j++
			}
			emit(line[i:j], pg.Tokens.String)
			i = j
			continue
		}
		if c >= '0' && c <= '9' {
			j := i + 1
			for j < n && (isIdentCont(line[j]) || line[j] == '.' || line[j] == '-') {
				j++
			}
			emit(line[i:j], pg.Tokens.Number)
			i = j
			continue
		}
		if isIdentStart(c) {
			j := i + 1
			for j < n && (isIdentCont(line[j]) || line[j] == '-') {
				j++
			}
			tok := line[i:j]
			if tok == "true" || tok == "false" || tok == "null" || tok == "nil" {
				emit(tok, pg.Tokens.Keyword)
			} else {
				emit(tok, pg.CodeText)
			}
			i = j
			continue
		}
		emit(line[i:i+1], pg.CodeText)
		i++
	}
	return spans
}

func lexMarkdownLine(line string, pg Page) []ui.Span {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "#") {
		return []ui.Span{{Text: line, Color: pg.Tokens.Keyword}}
	}
	if strings.HasPrefix(trimmed, ">") {
		return []ui.Span{{Text: line, Color: pg.Tokens.Comment}}
	}
	var spans []ui.Span
	emit := func(text string, color ui.Color) {
		if text == "" {
			return
		}
		if len(spans) > 0 && spans[len(spans)-1].Color == color {
			spans[len(spans)-1].Text += text
		} else {
			spans = append(spans, ui.Span{Text: text, Color: color})
		}
	}
	for i := 0; i < len(line); {
		if line[i] == '`' {
			if end := strings.IndexByte(line[i+1:], '`'); end >= 0 {
				emit(line[i:i+1+end+1], pg.Tokens.String)
				i += 1 + end + 1
				continue
			}
		}
		if strings.HasPrefix(line[i:], "**") {
			if end := strings.Index(line[i+2:], "**"); end >= 0 {
				emit(line[i:i+2+end+2], pg.Tokens.Type)
				i += 2 + end + 2
				continue
			}
		}
		if line[i] == '[' {
			if closeBr := strings.IndexByte(line[i+1:], ']'); closeBr >= 0 {
				after := i + 1 + closeBr + 1
				if after < len(line) && line[after] == '(' {
					if closePar := strings.IndexByte(line[after+1:], ')'); closePar >= 0 {
						emit(line[i:after+1+closePar+1], pg.Tokens.Function)
						i = after + 1 + closePar + 1
						continue
					}
				}
			}
		}
		emit(line[i:i+1], pg.CodeText)
		i++
	}
	return spans
}

var mermaidKeywords = wordSet("flowchart graph sequenceDiagram subgraph end direction participant actor autonumber activate deactivate loop alt else opt par and rect Note left right of over as TD TB LR RL BT")

func lexMermaidLine(line string, pg Page) []ui.Span {
	var spans []ui.Span
	emit := func(text string, color ui.Color) {
		if text == "" {
			return
		}
		if len(spans) > 0 && spans[len(spans)-1].Color == color {
			spans[len(spans)-1].Text += text
		} else {
			spans = append(spans, ui.Span{Text: text, Color: color})
		}
	}
	n := len(line)
	for i := 0; i < n; {
		if i+1 < n && line[i] == '%' && line[i+1] == '%' {
			emit(line[i:], pg.Tokens.Comment)
			break
		}
		if line[i] == '"' {
			j := i + 1
			for j < n && line[j] != '\n' {
				if line[j] == '\\' && j+1 < n {
					j += 2
					continue
				}
				if line[j] == '"' {
					j++
					break
				}
				j++
			}
			emit(line[i:j], pg.Tokens.String)
			i = j
			continue
		}
		if isIdentStart(line[i]) {
			j := i + 1
			for j < n && isIdentCont(line[j]) {
				j++
			}
			tok := line[i:j]
			if mermaidKeywords[tok] {
				emit(tok, pg.Tokens.Keyword)
			} else {
				emit(tok, pg.CodeText)
			}
			i = j
			continue
		}
		if strings.HasPrefix(line[i:], "-->") || strings.HasPrefix(line[i:], "-.->") || strings.HasPrefix(line[i:], "==>") ||
			strings.HasPrefix(line[i:], "->>") || strings.HasPrefix(line[i:], "-->>") || strings.HasPrefix(line[i:], "<-->") ||
			strings.HasPrefix(line[i:], "---") {
			j := i
			for j < n && (line[j] == '-' || line[j] == '.' || line[j] == '=' || line[j] == '>' || line[j] == '<' || line[j] == 'x' || line[j] == 'o') {
				j++
			}
			emit(line[i:j], pg.Tokens.Function)
			i = j
			continue
		}
		emit(line[i:i+1], pg.CodeText)
		i++
	}
	return spans
}
