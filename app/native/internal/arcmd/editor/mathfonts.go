package editor

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Textus uses these KaTeX faces. Embedding them keeps native formulas
// consistent on machines without mathematical fonts installed.
//
//go:embed fonts/KaTeX_Main-Regular.ttf fonts/KaTeX_Math-Italic.ttf
//go:embed fonts/KaTeX_Main-Bold.ttf fonts/KaTeX_Math-BoldItalic.ttf
var mathFonts embed.FS

// MathFontData returns the raw TTF bytes of the embedded KaTeX faces
// (Main Regular, Main Bold, Math Italic, and Math BoldItalic) for vector PDF
// embedding.
func MathFontData() (regular, bold, mathItalic, mathBoldItalic []byte) {
	regular, _ = mathFonts.ReadFile("fonts/KaTeX_Main-Regular.ttf")
	bold, _ = mathFonts.ReadFile("fonts/KaTeX_Main-Bold.ttf")
	mathItalic, _ = mathFonts.ReadFile("fonts/KaTeX_Math-Italic.ttf")
	mathBoldItalic, _ = mathFonts.ReadFile("fonts/KaTeX_Math-BoldItalic.ttf")
	return regular, bold, mathItalic, mathBoldItalic
}

var loadMathFonts = sync.OnceValue(func() error {
	for _, name := range []string{"KaTeX_Main-Regular", "KaTeX_Math-Italic", "KaTeX_Main-Bold", "KaTeX_Math-BoldItalic"} {
		data, err := mathFonts.ReadFile("fonts/" + name + ".ttf")
		if err != nil {
			return err
		}
		if err := ui.RegisterFont(data, ""); err != nil {
			return err
		}
	}
	return nil
})
