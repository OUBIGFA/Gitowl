package tex

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// fixed measures every rune half its size wide, reaching 0.8 of it up and
// 0.2 down, as a monospace font would.
func fixed(s string, size float32, italic, bold bool) (float32, float32, float32) {
	return float32(utf8.RuneCountInString(s)) * size * 0.5, size * 0.8, size * 0.2
}

func texts(b Box) []string {
	var out []string
	for _, op := range b.Ops {
		if op.Kind == "text" {
			out = append(out, op.Text)
		}
	}
	return out
}

func kinds(b Box, kind string) int {
	n := 0
	for _, op := range b.Ops {
		if op.Kind == kind {
			n++
		}
	}
	return n
}

func layout(t *testing.T, src string) Box {
	t.Helper()
	b, err := Layout(src, 20, fixed)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	if b.W <= 0 || b.Ascent <= 0 {
		t.Fatalf("%s: empty box %+v", src, b)
	}
	return b
}

func TestSymbolsAndScripts(t *testing.T) {
	b := layout(t, `E = mc^2 + \alpha_i^{n} - \infty`)
	got := strings.Join(texts(b), " ")
	if got != "E = m c 2 + α n i − ∞" {
		t.Errorf("texts %q", got)
	}
	var sup, base Op
	for _, op := range b.Ops {
		switch op.Text {
		case "2":
			sup = op
		case "c":
			base = op
		}
	}
	if sup.Size >= base.Size || sup.Y >= base.Y || sup.X <= base.X {
		t.Errorf("superscript %+v beside %+v", sup, base)
	}
	// Variables are italic, numbers and operators not.
	for _, op := range b.Ops {
		if want := op.Text == "E" || op.Text == "m" || op.Text == "c" || op.Text == "α" || op.Text == "n" || op.Text == "i"; op.Italic != want {
			t.Errorf("%q italic %v", op.Text, op.Italic)
		}
	}
}

func TestStructures(t *testing.T) {
	frac := layout(t, `\frac{a+b}{2}`)
	if kinds(frac, "rule") != 1 {
		t.Error("fraction without its bar")
	}
	var num, den Op
	for _, op := range frac.Ops {
		if op.Text == "a" {
			num = op
		}
		if op.Text == "2" {
			den = op
		}
	}
	if num.Y >= den.Y {
		t.Errorf("numerator %v not above denominator %v", num.Y, den.Y)
	}
	if kinds(layout(t, `\sqrt[3]{x}`), "path") != 1 {
		t.Error("root without its sign")
	}
	sum := layout(t, `\sum_{i=1}^{n} i`)
	var op, lower Op
	for _, o := range sum.Ops {
		if o.Text == "∑" {
			op = o
		}
		if o.Text == "1" {
			lower = o
		}
	}
	if op.Size <= 20 || lower.Y <= op.Y {
		t.Errorf("sum %+v, lower limit %+v", op, lower)
	}
	m := layout(t, `\begin{pmatrix} 1 & 2 \\ 3 & 4 \end{pmatrix}`)
	if strings.Join(texts(m), "") != "1234" || kinds(m, "path") != 2 {
		t.Errorf("matrix %q, %d paths", texts(m), kinds(m, "path"))
	}
	cases := layout(t, `f(x) = \begin{cases} 1 & x > 0 \\ 0 & \text{otherwise} \end{cases}`)
	if !strings.Contains(strings.Join(texts(cases), ""), "otherwise") {
		t.Errorf("cases %q", texts(cases))
	}
	left := layout(t, `\left( \frac{1}{2} \right)`)
	if kinds(left, "path") != 2 {
		t.Errorf("tall delimiters drawn as %d paths", kinds(left, "path"))
	}
	lines := layout(t, `a = b \\ c = d`)
	if lines.Descent < 20 {
		t.Errorf("second line not below: %+v", lines)
	}
	layout(t, `\lim_{x \to 0} \frac{\sin x}{x} = 1, \hat{x}, \vec{v}, \overline{AB}, \mathbb{R}, \binom{n}{k}, \int_0^1 x\,dx`)
}

func TestErrors(t *testing.T) {
	for _, src := range []string{`\frac{a}`, `{a`, `a}`, `\left( x`, `\begin{matrix} a`, `x^`, `\sqrt[2`} {
		if _, err := Layout(src, 20, fixed); err == nil {
			t.Errorf("%q laid out", src)
		}
	}
	b := layout(t, `\unknown + 1`)
	if b.Ops[0].Text != `\unknown` || !b.Ops[0].Error {
		t.Errorf("unknown command %+v", b.Ops[0])
	}
}

func TestIntegralLimitsClearUpperHook(t *testing.T) {
	b := layout(t, `\int_0^\infty x^2 e^{-x} dx = 2`)
	var upper, lower Op
	for _, op := range b.Ops {
		if op.Error {
			t.Fatalf("unrecognized token %q", op.Text)
		}
		if op.Text == "∞" {
			upper = op
		}
		if op.Text == "0" {
			lower = op
		}
	}
	if upper.X <= lower.X || upper.Y >= lower.Y {
		t.Fatal("integral limits do not follow its slant")
	}
}
