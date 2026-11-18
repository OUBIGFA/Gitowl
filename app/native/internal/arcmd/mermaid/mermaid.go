// Package mermaid parses and lays out a core subset of Mermaid diagrams
// (flowchart/graph and sequenceDiagram) into pure vector primitives without
// any UI toolkit dependency.
package mermaid

import (
	"errors"
	"fmt"
	"strings"
)

// MeasureFunc measures the rendered width and height (in DIPs) of text at the
// given font size and weight.
type MeasureFunc func(text string, size float32, bold bool) (w, h float32)

// Role identifies the semantic purpose of a primitive so callers can apply
// light/dark theme tokens consistently.
type Role uint8

const (
	RoleNode Role = iota
	RoleSubgraph
	RoleActor
	RoleNote
	RoleFragment
	RoleActivation
	RoleEdgeLabel
	RoleEdge
)

// ShapeKind identifies a vector node/container shape.
type ShapeKind uint8

const (
	ShapeRect ShapeKind = iota
	ShapeRound
	ShapeStadium
	ShapeSubroutine
	ShapeCylinder
	ShapeCircle
	ShapeDiamond
	ShapeHexagon
	ShapeParallelogram
	ShapeActor
	ShapePentagon
)

// EdgeKind selects solid, dashed, or thick stroke styling.
type EdgeKind uint8

const (
	EdgeSolid EdgeKind = iota
	EdgeDashed
	EdgeThick
)

// MarkerKind selects endpoint markers for paths.
type MarkerKind uint8

const (
	MarkerNone MarkerKind = iota
	MarkerArrow
	MarkerOpenArrow
	MarkerCircle
	MarkerCross
)

// ShapeOp describes a vector shape in diagram coordinates.
type ShapeOp struct {
	Kind   ShapeKind
	Role   Role
	X      float32
	Y      float32
	W      float32
	H      float32
	Radius float32
	Dashed bool
}

// PathOp describes an open polyline or smooth curve with optional endpoints.
type PathOp struct {
	Points      [][2]float32
	Smooth      bool
	Style       EdgeKind
	StartMarker MarkerKind
	EndMarker   MarkerKind
	Role        Role
}

// LabelOp describes a positioned multi-line or single-line text label.
type LabelOp struct {
	Text  string
	X     float32
	Y     float32
	W     float32
	H     float32
	Size  float32
	Bold  bool
	Align int // 0 center, -1 left, 1 right
	Role  Role
	Pill  bool
}

// Diagram holds the measured size and drawing primitives of a parsed diagram.
type Diagram struct {
	Kind   string // "flowchart" or "sequence"
	W      float32
	H      float32
	Shapes []ShapeOp
	Paths  []PathOp
	Labels []LabelOp
}

// ParseAndLayout parses Mermaid source and computes vector coordinates using measure.
func ParseAndLayout(src string, measure MeasureFunc) (Diagram, error) {
	if measure == nil {
		measure = defaultMeasure
	}
	lines := preprocessLines(src)
	if len(lines) == 0 {
		return Diagram{}, errors.New("empty diagram")
	}
	head := lines[0]
	fields := strings.Fields(head)
	if len(fields) == 0 {
		return Diagram{}, errors.New("empty diagram")
	}
	kw := strings.ToLower(fields[0])
	switch kw {
	case "flowchart", "graph":
		return layoutFlowchart(lines, measure)
	case "sequencediagram":
		return layoutSequence(lines, measure)
	default:
		return Diagram{}, fmt.Errorf("unsupported diagram %q", fields[0])
	}
}

func defaultMeasure(text string, size float32, _ bool) (float32, float32) {
	lines := strings.Split(text, "\n")
	maxW := float32(0)
	for _, ln := range lines {
		w := float32(0)
		for _, r := range ln {
			if r >= 0x1100 {
				w += size
			} else {
				w += size * 0.56
			}
		}
		maxW = max(maxW, w)
	}
	return maxW, float32(len(lines)) * size * 1.35
}

func preprocessLines(src string) []string {
	// Normalize Windows line endings and strip optional %%{init:...}%% blocks.
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	for {
		start := strings.Index(src, "%%{")
		if start < 0 {
			break
		}
		end := strings.Index(src[start+3:], "}%%")
		if end < 0 {
			break
		}
		src = src[:start] + src[start+3+end+3:]
	}
	rawLines := strings.Split(src, "\n")
	var out []string
	for _, ln := range rawLines {
		// Strip trailing %% comments when not inside quotes.
		clean := stripLineComment(ln)
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}
		// Split semicolon-separated statements when outside quotes.
		for _, part := range splitSemicolons(clean) {
			p := strings.TrimSpace(part)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func stripLineComment(s string) string {
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			continue
		}
		if c == '%' && i+1 < len(s) && s[i+1] == '%' {
			return s[:i]
		}
	}
	return s
}

func splitSemicolons(s string) []string {
	var parts []string
	inQuote := byte(0)
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			continue
		}
		switch c {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func normalizeLabel(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	for _, br := range []string{"<br/>", "<br />", "<br>", "<BR/>", "<BR>", `\n`} {
		s = strings.ReplaceAll(s, br, "\n")
	}
	return s
}
