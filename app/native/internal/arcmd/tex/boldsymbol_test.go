package tex

import (
	"math"
	"strings"
	"testing"
)

func TestBoldsymbolParsesMath(t *testing.T) {
	for _, tc := range []struct {
		source, want string
	}{
		{`\boldsymbol{Z}`, "Z"},
		{`\boldsymbol{\mu}`, "μ"},
		{`\boldsymbol\mu`, "μ"},
		{`\boldsymbol{x_i}+y`, "xi+y"},
		{`\boldsymbol{\frac{x}{\mu}}`, "xμ"},
		{`\boldsymbol{\begin{pmatrix}x\\\mu\end{pmatrix}}`, "xμ"},
		{`\boldsymbol{\boldsymbol{x}+y}+z`, "x+y+z"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			for _, render := range []func(string, float32, Measure) (Box, error){Layout, LayoutInline} {
				b, err := render(tc.source, 20, fixed)
				if err != nil {
					t.Fatal(err)
				}
				for _, op := range b.Ops {
					if op.Error {
						t.Fatalf("unsupported token %q", op.Text)
					}
				}
				if got := strings.Join(texts(b), ""); got != tc.want {
					t.Fatalf("rendered %q, want %q", got, tc.want)
				}
			}
		})
	}
}

func TestBoldsymbolStyleScope(t *testing.T) {
	for _, tc := range []struct {
		source string
		bold   []bool
		italic []bool
	}{
		{`\boldsymbol{x}_i+y`, []bool{true, false, false, false}, []bool{true, true, false, true}},
		{`\boldsymbol{x_i}+y`, []bool{true, true, false, false}, []bool{true, true, false, true}},
		{`\boldsymbol{\mu+1}+\Sigma`, []bool{true, true, true, false, false}, []bool{true, false, false, false, false}},
		{`\boldsymbol{\frac{x}{y}}+z`, []bool{true, true, false, false}, []bool{true, true, false, true}},
		{`\boldsymbol{\begin{pmatrix}x\\\mu\end{pmatrix}}+z`, []bool{true, true, false, false}, []bool{true, true, false, true}},
		{`\boldsymbol{\boldsymbol{x}+y}+z`, []bool{true, true, true, false, false}, []bool{true, false, true, false, true}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			b := layout(t, tc.source)
			i := 0
			for _, op := range b.Ops {
				if op.Kind != "text" {
					continue
				}
				if i >= len(tc.bold) || op.Error || op.Bold != tc.bold[i] || op.Italic != tc.italic[i] {
					t.Fatalf("unexpected style at text %d: %+v", i, op)
				}
				i++
			}
			if i != len(tc.bold) {
				t.Fatalf("got %d text operations, want %d", i, len(tc.bold))
			}
		})
	}
}

func TestBoldsymbolMeasuresBeforePlacement(t *testing.T) {
	measure := func(s string, size float32, italic, bold bool) (float32, float32, float32) {
		w, a, d := fixed(s, size, italic, bold)
		if bold {
			w *= 2
		}
		return w, a, d
	}
	b, err := Layout(`\boldsymbol{x}y`, 20, measure)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Ops) != 2 || math.Abs(float64(b.Ops[1].X-20.8)) > .001 || math.Abs(float64(b.W-31.6)) > .001 {
		t.Fatalf("placement did not use bold advance: %+v", b)
	}
}

func TestBoldsymbolPreservesOperatorSpacing(t *testing.T) {
	for _, op := range []string{"+", "="} {
		plain := layout(t, "a"+op+"b")
		bold := layout(t, `a\boldsymbol{`+op+`}b`)
		if plain.W != bold.W || plain.Ops[1].X != bold.Ops[1].X || plain.Ops[2].X != bold.Ops[2].X {
			t.Fatalf("spacing changed around %s: plain %+v, bold %+v", op, plain, bold)
		}
	}
}

func TestBoldsymbolRequiresArgument(t *testing.T) {
	for _, source := range []string{`\boldsymbol`, `\boldsymbol{x`, `\boldsymbol^2`} {
		if _, err := Layout(source, 20, fixed); err == nil {
			t.Errorf("accepted malformed formula %q", source)
		}
	}
}

func TestBoldsymbolOperatorLimits(t *testing.T) {
	for _, tc := range []struct {
		source, operator, lower string
		display, centered       bool
	}{
		{`\boldsymbol{\sum}_{i=1}^n`, "∑", "i", true, true},
		{`\boldsymbol{\lim}_{x\to 0}`, "lim", "x", true, true},
		{`\boldsymbol{\sum}\limits_{i=1}^n`, "∑", "i", true, true},
		{`\boldsymbol{\sum}\nolimits_{i=1}^n`, "∑", "i", true, false},
		{`\boldsymbol{\sum}_{i=1}^n`, "∑", "i", false, false},
		{`\boldsymbol{\lim}_{x\to 0}`, "lim", "x", false, true},
		{`\boldsymbol{\boldsymbol{\sum}}_{i=1}^n`, "∑", "i", true, true},
		{`\boldsymbol{\boldsymbol{\sum}\nolimits}_{i=1}^n`, "∑", "i", true, false},
		{`\boldsymbol{\boldsymbol{\sum}\nolimits}\limits_{i=1}^n`, "∑", "i", true, true},
		{`\boldsymbol{\int}\limits_0^\infty`, "∫", "0", true, true},
	} {
		mode := "inline"
		if tc.display {
			mode = "display"
		}
		t.Run(mode+"/"+tc.source, func(t *testing.T) {
			b, err := layoutMode(tc.source, 20, fixed, tc.display)
			if err != nil {
				t.Fatal(err)
			}
			var operator, lower Op
			for _, op := range b.Ops {
				if op.Error {
					t.Fatalf("unsupported token %q", op.Text)
				}
				if op.Text == tc.operator {
					operator = op
				}
				if op.Text == tc.lower {
					lower = op
				}
			}
			if operator.Text == "" || lower.Text == "" || !operator.Bold || lower.Bold || lower.Y <= operator.Y {
				t.Fatalf("operator or outer script lost its style/position: %+v, %+v", operator, lower)
			}
			width, _, _ := fixed(operator.Text, operator.Size, operator.Italic, operator.Bold)
			if centered := lower.X < operator.X+width; centered != tc.centered {
				t.Fatalf("centered = %v, want %v: operator %+v, lower %+v", centered, tc.centered, operator, lower)
			}
		})
	}
}

func TestBoldsymbolIntegralHookClearance(t *testing.T) {
	for _, source := range []string{`\boldsymbol{\int}_0^\infty`, `\boldsymbol{\boldsymbol{\int}}_0^\infty`} {
		for _, render := range []func(string, float32, Measure) (Box, error){Layout, LayoutInline} {
			b, err := render(source, 20, fixed)
			if err != nil {
				t.Fatal(err)
			}
			var upper, lower Op
			for _, op := range b.Ops {
				if op.Text == "∞" {
					upper = op
				}
				if op.Text == "0" {
					lower = op
				}
			}
			if upper.Text == "" || lower.Text == "" || upper.Bold || lower.Bold || upper.Y >= lower.Y || math.Abs(float64(upper.X-lower.X-4.4)) > .001 {
				t.Errorf("%s lost the integral's upper-hook clearance: upper %+v, lower %+v", source, upper, lower)
			}
		}
	}
}

func TestBoldsymbolDoesNotReuseScriptedOperator(t *testing.T) {
	for _, source := range []string{`\boldsymbol{\sum_i}^n`, `\boldsymbol{\boldsymbol{\sum_i}}\limits^n`} {
		b := layout(t, source)
		var operator, inner, outer Op
		for _, op := range b.Ops {
			switch op.Text {
			case "∑":
				operator = op
			case "i":
				inner = op
			case "n":
				outer = op
			}
		}
		width, _, _ := fixed(operator.Text, operator.Size, false, true)
		if operator.Text == "" || inner.Text == "" || outer.Text == "" || !inner.Bold || outer.Bold || inner.X >= operator.X+width || outer.X < operator.X+width {
			t.Fatalf("%s reused the inner operator for an outer script: %+v", source, b)
		}
	}
}
