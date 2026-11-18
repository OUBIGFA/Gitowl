package tex

type symbol struct {
	kind, text string
	italic     bool
}

var symbols = map[string]symbol{}

func init() {
	greek := map[string]string{
		"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ϵ", "varepsilon": "ε", "zeta": "ζ", "eta": "η",
		"theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ",
		"pi": "π", "varpi": "ϖ", "rho": "ρ", "varrho": "ϱ", "sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ",
		"phi": "ϕ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	}
	for name, s := range greek {
		symbols[name] = symbol{"ord", s, true}
	}
	for name, s := range map[string]string{
		"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π", "Sigma": "Σ",
		"Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
		"infty": "∞", "partial": "∂", "nabla": "∇", "emptyset": "∅", "varnothing": "∅", "forall": "∀", "exists": "∃",
		"nexists": "∄", "neg": "¬", "lnot": "¬", "ldots": "…", "cdots": "⋯", "vdots": "⋮", "ddots": "⋱", "dots": "…",
		"prime": "′", "angle": "∠", "degree": "°", "hbar": "ℏ", "ell": "ℓ", "Re": "ℜ", "Im": "ℑ", "aleph": "ℵ",
		"top": "⊤", "bot": "⊥", "triangle": "△", "square": "□", "checkmark": "✓", "star": "⋆", "dagger": "†",
		"#": "#", "$": "$", "%": "%", "_": "_", "&": "&", "|": "‖",
	} {
		symbols[name] = symbol{"ord", s, false}
	}
	for name, s := range map[string]string{
		"pm": "±", "mp": "∓", "times": "×", "div": "÷", "cdot": "·", "circ": "∘", "ast": "∗", "bullet": "∙",
		"cup": "∪", "cap": "∩", "setminus": "∖", "wedge": "∧", "land": "∧", "vee": "∨", "lor": "∨", "oplus": "⊕",
		"otimes": "⊗", "odot": "⊙",
	} {
		symbols[name] = symbol{"bin", s, false}
	}
	for name, s := range map[string]string{
		"leq": "≤", "le": "≤", "geq": "≥", "ge": "≥", "neq": "≠", "ne": "≠", "approx": "≈", "equiv": "≡", "sim": "∼",
		"simeq": "≃", "cong": "≅", "propto": "∝", "ll": "≪", "gg": "≫", "to": "→", "rightarrow": "→", "leftarrow": "←",
		"gets": "←", "Rightarrow": "⇒", "Leftarrow": "⇐", "leftrightarrow": "↔", "Leftrightarrow": "⇔", "iff": "⟺",
		"implies": "⟹", "mapsto": "↦", "longrightarrow": "⟶", "longleftarrow": "⟵", "uparrow": "↑", "downarrow": "↓",
		"in": "∈", "notin": "∉", "ni": "∋", "subset": "⊂", "subseteq": "⊆", "supset": "⊃", "supseteq": "⊇",
		"perp": "⊥", "parallel": "∥", "mid": "∣", "vdash": "⊢", "models": "⊨",
	} {
		symbols[name] = symbol{"rel", s, false}
	}
	for name, s := range map[string]string{"{": "{", "lbrace": "{", "langle": "⟨", "lfloor": "⌊", "lceil": "⌈"} {
		symbols[name] = symbol{"open", s, false}
	}
	for name, s := range map[string]string{"}": "}", "rbrace": "}", "rangle": "⟩", "rfloor": "⌋", "rceil": "⌉"} {
		symbols[name] = symbol{"close", s, false}
	}
}

// bigOps are the operators drawn larger in display math, with limits above
// and below but for integrals.
var bigOps = map[string]string{
	"sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫", "iint": "∬", "iiint": "∭", "oint": "∮",
	"bigcup": "⋃", "bigcap": "⋂", "bigoplus": "⨁", "bigotimes": "⨂", "bigvee": "⋁", "bigwedge": "⋀",
}

// functions are the names set upright, as \sin; limitFunctions take their
// subscript below, as \lim.
var functions = map[string]bool{
	"sin": true, "cos": true, "tan": true, "cot": true, "sec": true, "csc": true, "arcsin": true, "arccos": true,
	"arctan": true, "sinh": true, "cosh": true, "tanh": true, "log": true, "ln": true, "lg": true, "exp": true,
	"lim": true, "limsup": true, "liminf": true, "max": true, "min": true, "sup": true, "inf": true, "det": true,
	"dim": true, "ker": true, "deg": true, "gcd": true, "arg": true, "Pr": true, "mod": true, "bmod": true,
}

var limitFunctions = map[string]bool{
	"lim": true, "limsup": true, "liminf": true, "max": true, "min": true, "sup": true, "inf": true, "det": true, "gcd": true, "Pr": true,
}

// spaces are the spacing commands, in em.
var spaces = map[string]float32{
	",": 3.0 / 18, ":": 4.0 / 18, ">": 4.0 / 18, ";": 5.0 / 18, "!": -3.0 / 18, " ": 0.33, "quad": 1, "qquad": 2,
	"enspace": 0.5, "thinspace": 3.0 / 18,
}

// accents are drawn over their base: the character, or "line" and "arrow"
// for lines across it.
var accents = map[string]string{
	"hat": "ˆ", "widehat": "ˆ", "bar": "line", "overline": "line", "vec": "arrow", "overrightarrow": "arrow",
	"dot": "˙", "ddot": "¨", "tilde": "˜", "widetilde": "˜", "acute": "´", "grave": "`", "check": "ˇ", "breve": "˘",
	"underline": "under",
}

var blackboard = map[rune]rune{
	'R': 'ℝ', 'N': 'ℕ', 'Z': 'ℤ', 'Q': 'ℚ', 'C': 'ℂ', 'P': 'ℙ', 'H': 'ℍ', 'E': '𝔼', '1': '𝟙',
}

var calligraphic = map[rune]rune{
	'A': '𝒜', 'B': 'ℬ', 'C': '𝒞', 'D': '𝒟', 'E': 'ℰ', 'F': 'ℱ', 'G': '𝒢', 'H': 'ℋ', 'L': 'ℒ', 'M': 'ℳ',
	'N': '𝒩', 'O': '𝒪', 'P': '𝒫', 'R': 'ℛ', 'S': '𝒮', 'T': '𝒯',
}
