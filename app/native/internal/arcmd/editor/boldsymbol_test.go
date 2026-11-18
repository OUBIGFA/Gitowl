package editor

import (
	"math"
	"os"
	"runtime"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/tex"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func TestBoldsymbolNormalDistribution(t *testing.T) {
	data, err := os.ReadFile("testdata/normal-distribution.md")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	e, _ := newTester(source)
	if len(e.rows) != 3 || e.rows[1].layout == nil || len(e.rows[1].layout.objects) != 3 || e.rows[2].math == nil {
		t.Fatal("inline matrices or display density did not render")
	}
	boxes := []*tex.Box{e.rows[2].math}
	for _, obj := range e.rows[1].layout.objects {
		boxes = append(boxes, obj.box)
	}
	for _, box := range boxes {
		if box == nil || box.W <= 0 {
			t.Fatal("empty formula")
		}
		for _, op := range box.Ops {
			if op.Error {
				t.Fatalf("unsupported formula token %q", op.Text)
			}
		}
	}
	if got := ExportMarkdown(e.Doc); got != source {
		t.Fatalf("formula source changed during round trip: %q", got)
	}
}

func TestBoldsymbolUsesEmbeddedBoldFaces(t *testing.T) {
	if err := loadMathFonts(); err != nil {
		t.Skip(err)
	}
	e := New("")
	for _, tc := range []struct {
		text, file string
		italic     bool
	}{
		{"Z", "KaTeX_Math-BoldItalic", true},
		{"μ", "KaTeX_Math-BoldItalic", true},
		{"Σ", "KaTeX_Main-Bold", false},
		{"1", "KaTeX_Main-Bold", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			data, err := mathFonts.ReadFile("fonts/" + tc.file + ".ttf")
			if err != nil {
				t.Fatal(err)
			}
			face, err := sfnt.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			glyph, err := face.GlyphIndex(nil, []rune(tc.text)[0])
			if err != nil || glyph == 0 {
				t.Fatalf("missing bold glyph: %v", err)
			}
			advance, err := face.GlyphAdvance(nil, glyph, fixed.I(40), font.HintingNone)
			if err != nil {
				t.Fatal(err)
			}
			regular := e.glyphs(tc.text, 40, tc.italic, false)
			bold := e.glyphs(tc.text, 40, tc.italic, true)
			got, _ := bold.Size()
			want := float64(advance) / 64
			if runtime.GOOS == "linux" {
				// MyGo's Pango backend enables whole-pixel metric hinting.
				want = math.Round(want)
			}
			if math.Abs(float64(got)-want) > .05 {
				t.Fatalf("bold advance %g does not match embedded face %g", got, want)
			}
			if regular == bold || e.glyphs(tc.text, 40, tc.italic, false) != regular {
				t.Fatal("bold and regular glyphs share a cache entry")
			}
		})
	}
}
