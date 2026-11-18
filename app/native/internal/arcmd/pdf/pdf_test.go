package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func TestWrite(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))
	img.Set(1, 1, color.Black)
	var out bytes.Buffer
	if err := Write(&out, "计划 🚀", []Page{{Image: img, Width: A4Width, Height: A4Height}, {Image: img, Width: A4Width, Height: A4Height}}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"%PDF-1.4", "/Count 2", "/MediaBox [0 0 595.28 841.89]", "/DCTDecode", "<FEFF8BA152120020D83DDE80>", "%%EOF"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Every object the xref names starts where it says.
	xref := strings.Index(s, "\nxref\n") + 1
	lines := strings.Split(s[xref:], "\n")[3:]
	for i, l := range lines[:13] {
		var off int
		if _, err := fmtSscan(l, &off); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(s[off:], itoa(i+1)+" 0 obj") {
			t.Errorf("object %d not at %d", i+1, off)
		}
	}
	if err := Write(&out, "", nil); err == nil {
		t.Error("wrote no pages")
	}
}

func fmtSscan(s string, off *int) (int, error) {
	n, err := strconv.Atoi(strings.Fields(s)[0])
	*off = n
	return 1, err
}
func itoa(n int) string { return fmt.Sprint(n) }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("disk full") }

func TestStreamReportsFailureAndUnicode(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	w := NewWriter(failingWriter{}, "test")
	if err := w.AddPage(Page{Image: img, Width: 100, Height: 100}); err == nil {
		t.Fatal("write failure hidden")
	}
	var out bytes.Buffer
	w = NewWriter(&out, "test")
	for i := 0; i < 3; i++ {
		before := out.Len()
		if err := w.AddPage(Page{Image: img, Width: 100, Height: 100, Text: []Text{{Value: "中", X: 10, Y: 10, Width: 12, Height: 12}, {Value: "🚀", X: 22, Y: 10, Width: 12, Height: 12}}}); err != nil {
			t.Fatal(err)
		}
		if out.Len() <= before {
			t.Fatal("page was buffered until close")
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<0001> <4E2D>", "<0002> <D83DDE80>", "/Count 3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing", want)
		}
	}
}

func TestVectorPDFOutlinesLinksAndType3(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, "Vector Spec")
	w.ReservePages(2)
	w.SetOutlines([]OutlineItem{
		{Title: "Introduction", Page: 0, Y: 48, Level: 1},
		{Title: "Details", Page: 1, Y: 72, Level: 2},
	})

	p0 := w.NewVectorPage(A4Width, A4Height)
	p0.FillRect(48, 48, 499, 32, 6, [4]uint8{240, 245, 255, 255})
	p0.StrokeRect(48, 48, 499, 32, 6, 1, [4]uint8{30, 80, 180, 255}, false)
	p0.DrawText("Introduction Title 中文测试", 58, 70, 260, 16, TextStyle{Family: FontSans, Bold: true}, [4]uint8{20, 20, 20, 255})
	p0.AddLinkURI(58, 90, 140, 18, "https://example.com/spec")
	p0.AddLinkDest(58, 120, 120, 18, 1, 72)
	if err := w.FinishVectorPage(p0); err != nil {
		t.Fatal(err)
	}

	p1 := w.NewVectorPage(A4Width, A4Height)
	p1.DrawText("Monospace code := 42", 58, 72, 200, 12, TextStyle{Family: FontMono}, [4]uint8{30, 30, 30, 255})
	p1.DrawCircle(120, 140, 20, [4]uint8{200, 220, 255, 200}, [4]uint8{20, 60, 180, 255}, 1.5)
	if err := w.FinishVectorPage(p1); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	s := out.String()
	for _, want := range []string{"/Outlines", "/PageMode /UseOutlines", "/Subtype /Link", "/URI (https://example.com/spec)", "/Dest [", "/Filter /FlateDecode", "/Subtype /Type3", "/ToUnicode"} {
		if !strings.Contains(s, want) {
			t.Errorf("vector PDF missing %q", want)
		}
	}
	if strings.Contains(s, "/DCTDecode") {
		t.Errorf("expected no full-page JPEG raster stream on pure vector PDF pages")
	}

	// Verify xref table integrity for every object ID.
	xref := strings.Index(s, "\nxref\n") + 1
	lines := strings.Split(s[xref:], "\n")[2:]
	for i := 1; i < len(w.offsets); i++ {
		if !strings.HasPrefix(lines[i], "0") {
			t.Fatalf("invalid xref entry %d: %q", i, lines[i])
		}
		var off int
		if _, err := fmtSscan(lines[i], &off); err != nil {
			t.Fatal(err)
		}
		if off <= 0 || !strings.HasPrefix(s[off:], itoa(i)+" 0 obj") {
			t.Errorf("object %d offset %d mismatch: got %q", i, off, s[off:min(len(s), off+24)])
		}
	}
}

func TestRegisteredBoldFontSelection(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out, "Bold Math")
	w.RegisterFontData(FontMath, false, false, goregular.TTF)
	w.RegisterFontData(FontMath, true, false, gobold.TTF)
	p := w.NewVectorPage(A4Width, A4Height)
	p.DrawText("Z", 50, 100, 0, 16, TextStyle{Family: FontMath, Bold: false}, [4]uint8{0, 0, 0, 255})
	p.DrawText("Z", 80, 100, 0, 16, TextStyle{Family: FontMath, Bold: true}, [4]uint8{0, 0, 0, 255})
	if err := w.FinishVectorPage(p); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if len(w.fm.subsets) == 0 || len(w.fm.subsets[0].glyphs) != 2 {
		t.Fatalf("expected 2 distinct Type 3 glyphs for regular and bold registered faces, got subsets=%+v", w.fm.subsets)
	}
	g0, g1 := w.fm.subsets[0].glyphs[0], w.fm.subsets[0].glyphs[1]
	if bytes.Equal(g0.stream, g1.stream) {
		t.Fatal("bold glyph reused regular outline stream")
	}
	s := out.String()
	for _, want := range []string{"/g01", "/g02"} {
		if !strings.Contains(s, want) {
			t.Fatalf("vector PDF missing Type 3 glyph %q", want)
		}
	}
}
