// Package wiki finds the links between documents and the tags written in
// their text: [[Name]] links to the document named Name, as Obsidian's and
// Logseq's do, and #tag marks a document with a tag. Both are plain text in
// the document, so they survive Markdown and need no format of their own.
// The package has no dependency on the window or the document model.
package wiki

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Link is a [[target]] in a text. Start and End are the byte offsets of
// the whole link, brackets included; Target is the document's name, without
// an alias after "|" or a heading after "#", and Label what the link shows.
type Link struct {
	Start, End int
	Target     string
	Label      string
}

// Links returns the links in text, in order. A link stays on one line and
// holds no brackets.
func Links(text string) []Link {
	var out []Link
	for i := 0; i < len(text); {
		at := strings.Index(text[i:], "[[")
		if at < 0 {
			break
		}
		start := i + at
		rest := text[start+2:]
		end := strings.Index(rest, "]]")
		if end < 0 {
			break
		}
		inner := rest[:end]
		if strings.ContainsAny(inner, "[]\n") {
			// "[[a [[b]]": the link is the innermost.
			i = start + 1
			continue
		}
		if start > 0 && text[start-1] == '!' {
			// Obsidian embeds ("![[...]]") attach files or blocks rather than linking notes.
			i = start + 2 + end + 2
			continue
		}
		target, label := Parse(inner)
		if target != "" {
			out = append(out, Link{Start: start, End: start + 2 + end + 2, Target: target, Label: label})
		}
		i = start + 2 + end + 2
	}
	return out
}

// Parse splits what is inside a link's brackets into the document's name
// and the label it shows: "Name|label", "Name#heading".
func Parse(inner string) (target, label string) {
	target, alias, hasAlias := strings.Cut(inner, "|")
	target, _, _ = strings.Cut(target, "#")
	target = strings.TrimSpace(target)
	label = target
	if hasAlias && strings.TrimSpace(alias) != "" {
		label = strings.TrimSpace(alias)
	}
	return target, label
}

// Same reports whether two names are the same document's: case and the
// spaces around them do not count.
func Same(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// LinksTo reports whether text links to the document named name.
func LinksTo(text, name string) bool {
	if !strings.Contains(text, "[[") {
		return false
	}
	for _, l := range Links(text) {
		if Same(l.Target, name) {
			return true
		}
	}
	return false
}

// Rename makes the links to the document named from link to the one named
// to, keeping their aliases and headings, and reports whether any changed.
func Rename(text, from, to string) (string, bool) {
	links := Links(text)
	var b strings.Builder
	last, changed := 0, false
	for _, l := range links {
		if !Same(l.Target, from) {
			continue
		}
		inner := text[l.Start+2 : l.End-2]
		// Keep whatever follows the name: "#heading", "|alias".
		cut := len(inner)
		if i := strings.IndexAny(inner, "#|"); i >= 0 {
			cut = i
		}
		b.WriteString(text[last:l.Start])
		b.WriteString("[[" + to + inner[cut:] + "]]")
		last, changed = l.End, true
	}
	if !changed {
		return text, false
	}
	b.WriteString(text[last:])
	return b.String(), true
}

// Tag is a #tag in a text: Start and End are its byte offsets, the "#"
// included, and Name the tag without it.
type Tag struct {
	Start, End int
	Name       string
}

// tagRune reports whether r may be part of a tag's name: letters (Chinese
// included), digits, and _ - /.
func tagRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/'
}

// Tags returns the tags in text, in order. A tag starts a line or follows a
// space or punctuation, so "C#" and "a#b" and URLs' fragments are none, and
// its name holds a letter, so "#1" is a number, not a tag.
func Tags(text string) []Tag {
	var out []Tag
	for i := 0; i < len(text); {
		at := strings.IndexByte(text[i:], '#')
		if at < 0 {
			break
		}
		start := i + at
		i = start + 1
		if start > 0 {
			// After a space or punctuation, as "。#旅行", but not after a
			// letter, a digit or a path ("C#", "a#b", "x/#frag").
			prev, _ := utf8.DecodeLastRuneInString(text[:start])
			if unicode.IsLetter(prev) || unicode.IsDigit(prev) || strings.ContainsRune("/#&_.:=?-@\\`'\"", prev) {
				continue
			}
		}
		end := start + 1
		letter := false
		for end < len(text) {
			r, n := utf8.DecodeRuneInString(text[end:])
			if !tagRune(r) {
				break
			}
			letter = letter || unicode.IsLetter(r)
			end += n
		}
		name := strings.TrimRight(text[start+1:end], "/-")
		if !letter || name == "" {
			continue
		}
		out = append(out, Tag{Start: start, End: start + 1 + len(name), Name: name})
		i = start + 1 + len(name)
	}
	return out
}

// TagNames returns the distinct tags of text, in the order first written,
// each as first spelled.
func TagNames(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range Tags(text) {
		k := strings.ToLower(t.Name)
		if !seen[k] {
			seen[k] = true
			out = append(out, t.Name)
		}
	}
	return out
}

// Open reports the start of an unfinished link before the end of text, as
// "see [[Pro", and what has been typed of its name: the editor offers the
// documents to complete it with.
func Open(text string) (start int, typed string, ok bool) {
	at := strings.LastIndex(text, "[[")
	if at < 0 || (at > 0 && text[at-1] == '!') {
		return 0, "", false
	}
	typed = text[at+2:]
	if strings.ContainsAny(typed, "[]\n") || utf8.RuneCountInString(typed) > 60 {
		return 0, "", false
	}
	return at, typed, true
}

// RenameTag renames the tag from, ignoring case, to to in text, and the
// tags under it with it: #工作/2026 follows #工作. A tag named to already
// is where from merges.
func RenameTag(text, from, to string) (string, bool) {
	from = strings.TrimPrefix(from, "#")
	to = strings.TrimPrefix(to, "#")
	if from == "" || to == "" {
		return text, false
	}
	var b strings.Builder
	last, changed := 0, false
	for _, t := range Tags(text) {
		name := t.Name
		rest := ""
		switch {
		case strings.EqualFold(name, from):
		case len(name) > len(from) && strings.EqualFold(name[:len(from)], from) && name[len(from)] == '/':
			rest = name[len(from):]
		default:
			continue
		}
		b.WriteString(text[last:t.Start])
		b.WriteString("#" + to + rest)
		last, changed = t.End, true
	}
	if !changed {
		return text, false
	}
	b.WriteString(text[last:])
	return b.String(), true
}

// ValidTag reports whether name can be a tag: what Tags reads as one whole.
func ValidTag(name string) bool {
	name = strings.TrimPrefix(name, "#")
	tags := Tags("#" + name)
	return len(tags) == 1 && tags[0].Name == name
}
