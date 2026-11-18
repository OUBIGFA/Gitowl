// Package document owns ArcMD's versioned rich document model and editing
// transactions. It has no dependency on the window or drawing toolkit.
package document

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
)

// CurrentVersion is the format this ArcMD writes. Version 2 added the color
// of callouts, version 3 the color and highlight of text, version 4 folded
// headings and list items, cell backgrounds, merged cells and callout
// emoji, version 5 the page's icon and cover, version 6 scripts, image widths
// and footnotes, version 7 hand-drawn text annotations, version 8 toggle
// blocks and the word goal. Older documents are read and migrated in memory, and written back
// as the current version only when edited.
const CurrentVersion = 8

// CoverGradients are the covers drawn rather than pictured, as
// "gradient:name" in Document.Cover.
var CoverGradients = []string{"dawn", "sea", "forest", "dusk", "sand", "ink"}

// CalloutColors are the colors a callout may have, after GitHub's alerts so
// Markdown keeps them: "" is a note, the default.
var CalloutColors = []string{"", "green", "purple", "yellow", "red"}

// TextColors are the colors text may have; TextHighlights the colors it
// may be marked with, light before strong.
var (
	TextColors     = []string{"gray", "red", "orange", "yellow", "green", "blue", "purple"}
	TextHighlights = []string{"light-gray", "light-red", "light-orange", "light-yellow", "light-green", "light-blue", "light-purple",
		"medium-gray", "gray", "red", "orange", "yellow", "green", "blue", "purple"}
)

// Annotations are the hand-drawn marks text may carry, after Opensource UI's
// AnnotatedText: drawn over the text, except a highlight, drawn under it.
var Annotations = []string{"wavy", "circle", "highlight", "underline", "line", "dotted", "double",
	"strike", "cross", "arrow", "bracket", "box"}

func oneOf(value string, set []string) bool {
	for _, v := range set {
		if v == value {
			return true
		}
	}
	return false
}

func validCalloutColor(color string) bool {
	for _, c := range CalloutColors {
		if c == color {
			return true
		}
	}
	return false
}

type Style struct {
	NoMath    bool   `json:"noMath,omitempty"` // Literal dollar signs from Markdown escapes.
	Script    string `json:"script,omitempty"` // "sup" or "sub" (version 6).
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Strike    bool   `json:"strike,omitempty"`
	Code      bool   `json:"code,omitempty"`
	Link      string `json:"link,omitempty"`
	// Color and Highlight name a text color and a highlight, one of
	// TextColors and TextHighlights (version 3).
	Color     string `json:"color,omitempty"`
	Highlight string `json:"highlight,omitempty"`
	// Annotation is a hand-drawn mark, one of Annotations, in
	// AnnotationColor, one of TextColors, or its own color when ""
	// (version 7).
	Annotation      string `json:"annotation,omitempty"`
	AnnotationColor string `json:"annotationColor,omitempty"`
}

type Run struct {
	Text  string `json:"text"`
	Style Style  `json:"style,omitempty"`
}

type Block struct {
	ImageWidth float32   `json:"imageWidth,omitempty"` // Preferred DIP width; zero is natural.
	NoteLabel  string    `json:"noteLabel,omitempty"`  // Footnote definition label (version 6).
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Runs       []Run     `json:"runs,omitempty"`
	Level      int       `json:"level,omitempty"`
	Indent     int       `json:"indent,omitempty"`
	Checked    bool      `json:"checked,omitempty"`
	Language   string    `json:"language,omitempty"`
	URL        string    `json:"url,omitempty"`
	Align      string    `json:"align,omitempty"`
	Widths     []float32 `json:"widths,omitempty"`
	// Color is a callout's color, one of CalloutColors (version 2).
	Color string `json:"color,omitempty"`
	// Folded hides what a heading or list item holds (version 4).
	Folded bool `json:"folded,omitempty"`
	// Background is a table cell's fill, one of TextHighlights; ColSpan
	// and RowSpan merge it with the cells right of and below it, which then
	// stay empty and out of the text (version 4).
	Background string `json:"background,omitempty"`
	ColSpan    int    `json:"colspan,omitempty"`
	RowSpan    int    `json:"rowspan,omitempty"`
	// Emoji is a callout's icon in place of its color's (version 4).
	Emoji    string   `json:"emoji,omitempty"`
	Children []*Block `json:"children,omitempty"`
}

type Document struct {
	Format  string   `json:"format"`
	Version int      `json:"version"`
	Blocks  []*Block `json:"blocks"`
	// Icon is the page's icon, an emoji above its title; Cover the band
	// above both: "gradient:name" of CoverGradients, or a picture beside
	// the document, as an image block's URL (version 5).
	Icon  string `json:"icon,omitempty"`
	Cover string `json:"cover,omitempty"`
	// CoverY is which part of a picture cover taller than its band shows:
	// 0 its middle, -1 its top, 1 its bottom.
	CoverY float64 `json:"coverY,omitempty"`
	// Goal is how many words the writer means the page to reach, 0 for
	// none (version 8).
	Goal int `json:"goal,omitempty"`
	// Frontmatter holds user YAML frontmatter keys preserved across Markdown
	// round-trips outside ArcMD's own page metadata.
	Frontmatter string `json:"frontmatter,omitempty"`
	// ImportSource preserves the original interchange file for recovery.
	ImportSource string `json:"importSource,omitempty"`
}

var (
	idMu   sync.Mutex
	idPool [4080]byte
	idUsed = len(idPool)
)

func NewID() string {
	var raw [12]byte
	idMu.Lock()
	if idUsed+len(raw) > len(idPool) {
		if _, err := rand.Read(idPool[:]); err != nil {
			idMu.Unlock()
			panic(err)
		}
		idUsed = 0
	}
	copy(raw[:], idPool[idUsed:idUsed+len(raw)])
	idUsed += len(raw)
	idMu.Unlock()
	var encoded [24]byte
	hex.Encode(encoded[:], raw[:])
	return string(encoded[:])
}

func NewBlock(kind, text string) *Block {
	return &Block{ID: NewID(), Kind: kind, Runs: []Run{{Text: text}}}
}

func New() *Document {
	return &Document{Format: "arcmd", Version: CurrentVersion, Blocks: []*Block{NewBlock("paragraph", "")}}
}

func (b *Block) Text() string {
	if len(b.Runs) == 1 {
		return b.Runs[0].Text
	}
	var out strings.Builder
	for _, run := range b.Runs {
		out.WriteString(run.Text)
	}
	return out.String()
}

func (b *Block) Clone() *Block {
	out := *b
	out.Runs = append([]Run(nil), b.Runs...)
	out.Widths = append([]float32(nil), b.Widths...)
	if b.Children != nil {
		out.Children = make([]*Block, len(b.Children))
	}
	for i, child := range b.Children {
		out.Children[i] = child.Clone()
	}
	return &out
}

func (d *Document) Clone() *Document {
	out := *d
	out.Blocks = make([]*Block, len(d.Blocks))
	for i, block := range d.Blocks {
		out.Blocks[i] = block.Clone()
	}
	return &out
}

func Decode(text string) (*Document, error) {
	var d Document
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return nil, fmt.Errorf("document: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("document: trailing data")
	}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	d.linkFiles()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// linkFiles turns the file blocks older documents may hold into
// paragraphs with a link to the file, as Markdown writes them: ArcMD no
// longer has attachments other than images.
func (d *Document) linkFiles() {
	var walk func([]*Block)
	walk = func(blocks []*Block) {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			if b.Kind == "file" {
				text := b.Text()
				if text == "" {
					text = b.URL
				}
				b.Kind = "paragraph"
				if text != "" {
					b.Runs = []Run{{Text: text, Style: Style{Link: b.URL}}}
				}
				b.URL = ""
			}
			walk(b.Children)
		}
	}
	walk(d.Blocks)
}

// migrate brings a document of an older version to CurrentVersion. A
// version 1 document has no colors; one that claims them is refused rather
// than guessed at.
func (d *Document) migrate() error {
	if d.Format != "arcmd" || d.Version < 1 || d.Version >= CurrentVersion {
		return nil
	}
	// A field an older version did not have means the file is damaged, not
	// newer: refuse it rather than guess.
	var blockColor, textColor func([]*Block) bool
	blockColor = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil && (b.Color != "" || blockColor(b.Children)) {
				return true
			}
		}
		return false
	}
	textColor = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			for _, r := range b.Runs {
				if r.Style.Color != "" || r.Style.Highlight != "" {
					return true
				}
			}
			if textColor(b.Children) {
				return true
			}
		}
		return false
	}
	if d.Version < 2 && blockColor(d.Blocks) {
		return errors.New("document: version 1 has no block colors")
	}
	if d.Version < 3 && textColor(d.Blocks) {
		return fmt.Errorf("document: version %d has no text colors", d.Version)
	}
	var newer func([]*Block) bool
	newer = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil && (b.Folded || b.Background != "" || b.ColSpan != 0 || b.RowSpan != 0 || b.Emoji != "" || newer(b.Children)) {
				return true
			}
		}
		return false
	}
	if d.Version < 4 && newer(d.Blocks) {
		return fmt.Errorf("document: version %d has no folds, cell styles or emoji", d.Version)
	}
	if d.Version < 5 && (d.Icon != "" || d.Cover != "" || d.CoverY != 0) {
		return fmt.Errorf("document: version %d has no page icon or cover", d.Version)
	}
	var v6 func([]*Block) bool
	v6 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil {
				if b.ImageWidth != 0 || b.NoteLabel != "" || b.Kind == "footnote" || v6(b.Children) {
					return true
				}
				for _, r := range b.Runs {
					if r.Style.Script != "" || r.Style.NoMath {
						return true
					}
				}
			}
		}
		return false
	}
	if d.Version < 6 && v6(d.Blocks) {
		return errors.New("document: scripts, image widths and footnotes require version 6")
	}
	var v7 func([]*Block) bool
	v7 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			for _, r := range b.Runs {
				if r.Style.Annotation != "" || r.Style.AnnotationColor != "" {
					return true
				}
			}
			if v7(b.Children) {
				return true
			}
		}
		return false
	}
	if d.Version < 7 && v7(d.Blocks) {
		return errors.New("document: annotations require version 7")
	}
	var v8 func([]*Block) bool
	v8 = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b != nil && (b.Kind == "toggle" || v8(b.Children)) {
				return true
			}
		}
		return false
	}
	if v8(d.Blocks) || d.Goal != 0 {
		return errors.New("document: toggle blocks and word goals require version 8")
	}
	d.Version = CurrentVersion
	return nil
}

func (d *Document) Encode() (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(d)
	return out.String(), err
}

// RunsDigest returns a 128-bit fingerprint of a block's styled runs without
// allocating or cloning run slices.
func RunsDigest(runs []Run) [16]byte {
	var h [32]byte
	d := sha256.New()
	var buf [64]byte
	hashRuns(d, &buf, runs)
	sum := d.Sum(h[:0])
	var out [16]byte
	copy(out[:], sum[:16])
	return out
}

// Digest validates the document and computes a SHA-256 fingerprint over all
// persisted document, block, and run fields without serializing JSON.
func (d *Document) Digest() ([32]byte, error) {
	if err := d.Validate(); err != nil {
		return [32]byte{}, err
	}
	h := sha256.New()
	var buf [64]byte
	hashDocHeader(h, &buf, d)
	for _, b := range d.Blocks {
		hashBlock(h, &buf, b)
	}
	var out [32]byte
	h.Sum(out[:0])
	return out, nil
}

// SkeletonDigest hashes the document's structure and metadata while omitting
// leaf blocks' Runs, matching the exact subset of fields that text-only edits
// cannot change. Thus Digest(a) == Digest(b) implies identical SkeletonDigests.
func (d *Document) SkeletonDigest() [32]byte {
	h := sha256.New()
	var buf [64]byte
	hashDocHeader(h, &buf, d)
	for _, b := range d.Blocks {
		hashBlockSkeleton(h, &buf, b)
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func hashDocHeader(h hash.Hash, buf *[64]byte, d *Document) {
	hashString(h, buf, d.Format)
	hashUint64(h, buf, uint64(d.Version))
	hashString(h, buf, d.Icon)
	hashString(h, buf, d.Cover)
	hashFloat64(h, buf, d.CoverY)
	hashUint64(h, buf, uint64(d.Goal))
	hashString(h, buf, d.Frontmatter)
	hashString(h, buf, d.ImportSource)
	hashUint64(h, buf, uint64(len(d.Blocks)))
}

func hashUint64(h hash.Hash, buf *[64]byte, v uint64) {
	binary.LittleEndian.PutUint64(buf[:8], v)
	_, _ = h.Write(buf[:8])
}

func hashFloat32(h hash.Hash, buf *[64]byte, v float32) {
	if v == 0 {
		v = 0
	}
	binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(v))
	_, _ = h.Write(buf[:4])
}

func hashFloat64(h hash.Hash, buf *[64]byte, v float64) {
	if v == 0 {
		v = 0
	}
	binary.LittleEndian.PutUint64(buf[:8], math.Float64bits(v))
	_, _ = h.Write(buf[:8])
}

func hashBool(h hash.Hash, buf *[64]byte, v bool) {
	if v {
		buf[0] = 1
	} else {
		buf[0] = 0
	}
	_, _ = h.Write(buf[:1])
}

func hashString(h hash.Hash, buf *[64]byte, s string) {
	hashUint64(h, buf, uint64(len(s)))
	_, _ = io.WriteString(h, s)
}

func hashRuns(h hash.Hash, buf *[64]byte, runs []Run) {
	hashUint64(h, buf, uint64(len(runs)))
	for i := range runs {
		r := &runs[i]
		hashString(h, buf, r.Text)
		st := &r.Style
		hashBool(h, buf, st.NoMath)
		hashString(h, buf, st.Script)
		hashBool(h, buf, st.Bold)
		hashBool(h, buf, st.Italic)
		hashBool(h, buf, st.Underline)
		hashBool(h, buf, st.Strike)
		hashBool(h, buf, st.Code)
		hashString(h, buf, st.Link)
		hashString(h, buf, st.Color)
		hashString(h, buf, st.Highlight)
		hashString(h, buf, st.Annotation)
		hashString(h, buf, st.AnnotationColor)
	}
}

func hashBlock(h hash.Hash, buf *[64]byte, b *Block) {
	hashBlockWithRuns(h, buf, b, true)
}

func hashBlockSkeleton(h hash.Hash, buf *[64]byte, b *Block) {
	hashBlockWithRuns(h, buf, b, false)
}

func hashBlockWithRuns(h hash.Hash, buf *[64]byte, b *Block, includeLeafRuns bool) {
	if b == nil {
		hashBool(h, buf, false)
		return
	}
	hashBool(h, buf, true)
	hashFloat32(h, buf, b.ImageWidth)
	hashString(h, buf, b.NoteLabel)
	hashString(h, buf, b.ID)
	hashString(h, buf, b.Kind)
	if includeLeafRuns || len(b.Children) > 0 {
		hashRuns(h, buf, b.Runs)
	}
	hashUint64(h, buf, uint64(b.Level))
	hashUint64(h, buf, uint64(b.Indent))
	hashBool(h, buf, b.Checked)
	hashString(h, buf, b.Language)
	hashString(h, buf, b.URL)
	hashString(h, buf, b.Align)
	hashUint64(h, buf, uint64(len(b.Widths)))
	for _, w := range b.Widths {
		hashFloat32(h, buf, w)
	}
	hashString(h, buf, b.Color)
	hashBool(h, buf, b.Folded)
	hashString(h, buf, b.Background)
	hashUint64(h, buf, uint64(b.ColSpan))
	hashUint64(h, buf, uint64(b.RowSpan))
	hashString(h, buf, b.Emoji)
	hashUint64(h, buf, uint64(len(b.Children)))
	for _, child := range b.Children {
		hashBlockWithRuns(h, buf, child, includeLeafRuns)
	}
}

// SameDocument reports whether a and b hold identical document metadata and
// block trees without reflection or allocations.
func SameDocument(a, b *Document) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Format != b.Format || a.Version != b.Version || a.Icon != b.Icon ||
		a.Cover != b.Cover || a.CoverY != b.CoverY || a.Goal != b.Goal ||
		a.Frontmatter != b.Frontmatter || a.ImportSource != b.ImportSource ||
		len(a.Blocks) != len(b.Blocks) {
		return false
	}
	for i := range a.Blocks {
		if !SameBlock(a.Blocks[i], b.Blocks[i]) {
			return false
		}
	}
	return true
}

// SameBlock reports whether two blocks and their descendants match exactly.
func SameBlock(a, b *Block) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.ImageWidth != b.ImageWidth || a.NoteLabel != b.NoteLabel ||
		a.ID != b.ID || a.Kind != b.Kind ||
		a.Level != b.Level || a.Indent != b.Indent ||
		a.Checked != b.Checked || a.Language != b.Language ||
		a.URL != b.URL || a.Align != b.Align ||
		a.Color != b.Color || a.Folded != b.Folded ||
		a.Background != b.Background || a.ColSpan != b.ColSpan ||
		a.RowSpan != b.RowSpan || a.Emoji != b.Emoji ||
		len(a.Widths) != len(b.Widths) || len(a.Runs) != len(b.Runs) ||
		len(a.Children) != len(b.Children) {
		return false
	}
	if !slices.Equal(a.Widths, b.Widths) || !slices.Equal(a.Runs, b.Runs) {
		return false
	}
	for i := range a.Children {
		if !SameBlock(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}

func (d *Document) Validate() error {
	if d.Format != "arcmd" || d.Version != CurrentVersion {
		return fmt.Errorf("document: unsupported format %q version %d", d.Format, d.Version)
	}
	if len(d.Blocks) == 0 {
		return errors.New("document: missing content")
	}
	if len(d.Icon) > 32 || strings.ContainsAny(d.Icon, "\n\r") {
		return errors.New("document: invalid page icon")
	}
	if !ValidCover(d.Cover) || !(d.CoverY >= -1 && d.CoverY <= 1) {
		return errors.New("document: invalid cover")
	}
	if d.Goal < 0 || d.Goal > 1000000 {
		return errors.New("document: invalid word goal")
	}
	ids := make(map[string]bool)
	var visit func(*Block, string, int) error
	visit = func(b *Block, parent string, depth int) error {
		if b == nil || depth > 24 {
			return errors.New("document: invalid block nesting")
		}
		if b.ID == "" || ids[b.ID] {
			return errors.New("document: missing or duplicate block ID")
		}
		ids[b.ID] = true
		if b.Level < 0 || b.Level > 6 || b.Indent < 0 || b.Indent > 12 {
			return errors.New("document: invalid block attributes")
		}
		if b.Align != "" && b.Align != "left" && b.Align != "center" && b.Align != "right" {
			return errors.New("document: invalid alignment")
		}
		if b.Color != "" && (b.Kind != "callout" || !validCalloutColor(b.Color)) {
			return errors.New("document: invalid block color")
		}
		if b.Folded && b.Kind != "heading" && b.Kind != "bullet" && b.Kind != "numbered" && b.Kind != "task" && b.Kind != "toggle" {
			return errors.New("document: only headings and list items fold")
		}
		if (b.Background != "" || b.ColSpan != 0 || b.RowSpan != 0) && b.Kind != "cell" {
			return errors.New("document: cell attributes outside a cell")
		}
		if b.Background != "" && !oneOf(b.Background, TextHighlights) {
			return errors.New("document: invalid cell background")
		}
		if b.ColSpan < 0 || b.RowSpan < 0 || b.ColSpan > 64 || b.RowSpan > 64 {
			return errors.New("document: invalid cell span")
		}
		if b.Emoji != "" && (b.Kind != "callout" || len(b.Emoji) > 32) {
			return errors.New("document: invalid callout emoji")
		}
		if !(b.ImageWidth >= 0 && b.ImageWidth <= 10000) || b.ImageWidth != 0 && b.Kind != "image" {
			return errors.New("document: invalid image width")
		}
		if b.Kind == "footnote" && (b.NoteLabel == "" || len(b.NoteLabel) > 1000 || strings.ContainsAny(b.NoteLabel, "[]\n\r\t ")) || b.Kind != "footnote" && b.NoteLabel != "" {
			return errors.New("document: invalid footnote label")
		}
		for _, r := range b.Runs {
			if r.Style.Script != "" && r.Style.Script != "sup" && r.Style.Script != "sub" {
				return errors.New("document: invalid script")
			}
			if r.Style.Color != "" && !oneOf(r.Style.Color, TextColors) || r.Style.Highlight != "" && !oneOf(r.Style.Highlight, TextHighlights) {
				return errors.New("document: invalid text color")
			}
			if r.Style.Annotation != "" && !oneOf(r.Style.Annotation, Annotations) ||
				r.Style.AnnotationColor != "" && (r.Style.Annotation == "" || !oneOf(r.Style.AnnotationColor, TextColors)) {
				return errors.New("document: invalid annotation")
			}
		}
		switch b.Kind {
		case "table":
			if len(b.Children) == 0 || b.Children[0] == nil || len(b.Children[0].Children) == 0 {
				return errors.New("document: empty table")
			}
			cols := len(b.Children[0].Children)
			for _, row := range b.Children {
				if row == nil || row.Kind != "row" || len(row.Children) != cols {
					return errors.New("document: ragged table")
				}
			}
			if len(b.Widths) != 0 && len(b.Widths) != cols {
				return errors.New("document: invalid column widths")
			}
			for _, width := range b.Widths {
				if width <= 0 || width > 10000 {
					return errors.New("document: invalid column width")
				}
			}
		case "row":
			if parent != "table" {
				return errors.New("document: row outside table")
			}
			for _, cell := range b.Children {
				if cell == nil || cell.Kind != "cell" {
					return errors.New("document: invalid table cell")
				}
			}
		case "columns":
			if len(b.Children) < 2 || len(b.Children) > 5 {
				return errors.New("document: invalid columns")
			}
			for _, column := range b.Children {
				if column == nil || column.Kind != "column" {
					return errors.New("document: invalid column")
				}
			}
			if len(b.Widths) != 0 && len(b.Widths) != len(b.Children) {
				return errors.New("document: invalid column widths")
			}
			for _, width := range b.Widths {
				if width <= 0 || width > 10000 {
					return errors.New("document: invalid column width")
				}
			}
		case "column":
			if parent != "columns" || len(b.Children) == 0 {
				return errors.New("document: invalid column content")
			}
		case "cell":
			if parent != "row" {
				return errors.New("document: cell outside row")
			}
		case "paragraph", "heading", "quote", "bullet", "numbered", "task", "code", "callout", "image", "rule", "raw", "math", "footnote", "toggle":
		default:
			return fmt.Errorf("document: unsupported block %q", b.Kind)
		}
		if b.Kind != "table" && b.Kind != "row" && b.Kind != "columns" && b.Kind != "column" && len(b.Children) != 0 {
			return errors.New("document: text block contains children")
		}
		for _, child := range b.Children {
			if err := visit(child, b.Kind, depth+1); err != nil {
				return err
			}
		}
		// Span checks dereference cells, so validate their structure first.
		if b.Kind == "table" {
			return validSpans(b)
		}
		return nil
	}
	for _, b := range d.Blocks {
		if err := visit(b, "root", 0); err != nil {
			return err
		}
	}
	return nil
}

// ValidCover reports whether a cover is one Document.Cover may hold: none,
// a gradient of CoverGradients, or a relative path beside the document.
func ValidCover(cover string) bool {
	if cover == "" {
		return true
	}
	if name, ok := strings.CutPrefix(cover, "gradient:"); ok {
		return oneOf(name, CoverGradients)
	}
	if len(cover) > 1024 || strings.Contains(cover, ":") || strings.HasPrefix(cover, "/") || strings.ContainsAny(cover, "\n\r\\") {
		return false
	}
	for _, part := range strings.Split(cover, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// Len and ByteOffset bridge the model's UTF-8 storage and native UTF-16 ranges.
func Len(text string) int {
	n := 0
	for _, r := range text {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func ByteOffset(text string, at int) int {
	units := 0
	for i, r := range text {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > at {
			return i
		}
		units += n
	}
	return len(text)
}

func SliceRuns(runs []Run, start, end int) []Run {
	var out []Run
	position := 0
	for _, run := range runs {
		n := Len(run.Text)
		a, z := max(0, start-position), min(n, end-position)
		if z > a {
			out = append(out, Run{Text: run.Text[ByteOffset(run.Text, a):ByteOffset(run.Text, z)], Style: run.Style})
		}
		position += n
	}
	return out
}

func Compact(runs []Run) []Run {
	if len(runs) == 0 {
		return nil
	}
	clean := true
	for i, run := range runs {
		if run.Text == "" || (i > 0 && runs[i-1].Style == run.Style) {
			clean = false
			break
		}
	}
	if clean {
		return slices.Clone(runs)
	}
	var out []Run
	for i := 0; i < len(runs); {
		if runs[i].Text == "" {
			i++
			continue
		}
		style := runs[i].Style
		j := i + 1
		nonEmpty := 1
		for j < len(runs) && (runs[j].Text == "" || runs[j].Style == style) {
			if runs[j].Text != "" {
				nonEmpty++
			}
			j++
		}
		if nonEmpty == 1 {
			out = append(out, Run{Text: runs[i].Text, Style: style})
		} else {
			var sb strings.Builder
			total := 0
			for k := i; k < j; k++ {
				total += len(runs[k].Text)
			}
			sb.Grow(total)
			for k := i; k < j; k++ {
				sb.WriteString(runs[k].Text)
			}
			out = append(out, Run{Text: sb.String(), Style: style})
		}
		i = j
	}
	return out
}

type Leaf struct {
	Block      *Block
	Parent     *[]*Block
	Index      int
	Owner      string
	Start, End int
	// ByteStart and ByteEnd address the same leaf in PlainText's UTF-8 string.
	ByteStart, ByteEnd int
}

func (d *Document) Leaves() []Leaf {
	out := make([]Leaf, 0, len(d.Blocks))
	position, bytePosition := 0, 0
	var walk func(*[]*Block, string, map[*Block]bool)
	walk = func(blocks *[]*Block, owner string, covered map[*Block]bool) {
		for i, block := range *blocks {
			top := owner
			if top == "" {
				top = block.ID
			}
			if covered[block] {
				// A merged-over cell is out of the text.
				continue
			}
			if len(block.Children) > 0 {
				inner := covered
				if block.Kind == "table" {
					inner = CoveredCells(block)
				}
				walk(&block.Children, top, inner)
				continue
			}
			text := block.Text()
			end, byteEnd := position+Len(text), bytePosition+len(text)
			out = append(out, Leaf{Block: block, Parent: blocks, Index: i, Owner: top, Start: position, End: end, ByteStart: bytePosition, ByteEnd: byteEnd})
			position = end + 1
			bytePosition = byteEnd + 1
		}
	}
	walk(&d.Blocks, "", nil)
	return out
}

func (d *Document) PlainText() string {
	// Walked directly rather than through Leaves: search rereads the text
	// of an open page as it changes, and on a long page the leaf list and
	// the slice of parts would be most of that work.
	var out strings.Builder
	first := true
	var walk func([]*Block, map[*Block]bool)
	walk = func(blocks []*Block, covered map[*Block]bool) {
		for _, block := range blocks {
			if covered[block] {
				continue
			}
			if len(block.Children) > 0 {
				inner := covered
				if block.Kind == "table" {
					inner = CoveredCells(block)
				}
				walk(block.Children, inner)
				continue
			}
			if !first {
				out.WriteByte('\n')
			}
			first = false
			for _, run := range block.Runs {
				out.WriteString(run.Text)
			}
		}
	}
	walk(d.Blocks, nil)
	return out.String()
}

// HasKind reports whether any block of the document, at any depth, is of
// the kind, without building the leaf list.
func (d *Document) HasKind(kind string) bool {
	var walk func([]*Block) bool
	walk = func(blocks []*Block) bool {
		for _, block := range blocks {
			if block.Kind == kind || walk(block.Children) {
				return true
			}
		}
		return false
	}
	return walk(d.Blocks)
}

func Table(rows, columns int) *Block {
	table := NewBlock("table", "")
	table.Runs = nil
	for row := 0; row < max(1, rows); row++ {
		line := &Block{ID: NewID(), Kind: "row"}
		for col := 0; col < max(1, columns); col++ {
			line.Children = append(line.Children, NewBlock("cell", ""))
		}
		table.Children = append(table.Children, line)
	}
	return table
}

func Columns() *Block {
	return &Block{ID: NewID(), Kind: "columns", Children: []*Block{
		{ID: NewID(), Kind: "column", Children: []*Block{NewBlock("paragraph", "")}},
		{ID: NewID(), Kind: "column", Children: []*Block{NewBlock("paragraph", "")}},
	}}
}

// Span is the number of columns and rows a cell spans: at least 1.
func Span(cell *Block) (cols, rows int) {
	return max(1, cell.ColSpan), max(1, cell.RowSpan)
}

// CoveredCells are the cells of table merged into another cell.
func CoveredCells(table *Block) map[*Block]bool {
	var out map[*Block]bool
	for r, row := range table.Children {
		for c, cell := range row.Children {
			cols, rows := Span(cell)
			if cols == 1 && rows == 1 {
				continue
			}
			for rr := r; rr < min(r+rows, len(table.Children)); rr++ {
				for cc := c; cc < min(c+cols, len(table.Children[rr].Children)); cc++ {
					if rr != r || cc != c {
						if out == nil {
							out = map[*Block]bool{}
						}
						out[table.Children[rr].Children[cc]] = true
					}
				}
			}
		}
	}
	return out
}

// validSpans checks a table's merged cells: inside the table, not
// overlapping, and the cells they cover empty.
func validSpans(table *Block) error {
	taken := map[[2]int]bool{}
	for r, row := range table.Children {
		for c, cell := range row.Children {
			if taken[[2]int{r, c}] {
				if cell.ColSpan > 1 || cell.RowSpan > 1 || cell.Text() != "" {
					return errors.New("document: merged cell over content")
				}
				continue
			}
			cols, rows := Span(cell)
			if r+rows > len(table.Children) || c+cols > len(row.Children) {
				return errors.New("document: merged cell outside its table")
			}
			for rr := r; rr < r+rows; rr++ {
				for cc := c; cc < c+cols; cc++ {
					if (rr != r || cc != c) && taken[[2]int{rr, cc}] {
						return errors.New("document: overlapping merged cells")
					}
					taken[[2]int{rr, cc}] = true
				}
			}
		}
	}
	return nil
}
