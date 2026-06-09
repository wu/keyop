//nolint:revive
package util

import (
	"fmt"
	"regexp"
	"strings"
)

// mathEntry holds a protected math expression pending post-goldmark rendering.
type mathEntry struct {
	latex   string
	display bool
}

var (
	mathDisplayRe    = regexp.MustCompile(`(?s)\$\$([\s\S]+?)\$\$`)
	mathInlineRe     = regexp.MustCompile(`\$(\S(?:[^$\n]*?\S)?|\S)\$`)
	mathCodeBlockRe  = regexp.MustCompile("(?m)^```[^\\n]*\\n[\\s\\S]*?^```\\s*$|^~~~[^\\n]*\\n[\\s\\S]*?^~~~\\s*$")
	mathCodeInlineRe = regexp.MustCompile("`[^`]+`")
	mathFracRe       = regexp.MustCompile(`\\frac\{([^}]*)\}\{([^}]*)\}`)
	mathTextCmdRe    = regexp.MustCompile(`\\(?:text|mathrm|mathit|mathbf|mathsf|mathtt|operatorname)\{([^}]*)\}`)
	mathMathbbRe     = regexp.MustCompile(`\\mathbb\{([A-Z])\}`)
	mathFontCmdRe    = regexp.MustCompile(`\\math(?:cal|scr|frak|bf|sf|tt|rm)\{([^}]*)\}`)
	mathSupBracesRe  = regexp.MustCompile(`\^\{([^{}]*)\}`)
	mathSubBracesRe  = regexp.MustCompile(`_\{([^{}]*)\}`)
	mathSupSingleRe  = regexp.MustCompile(`\^([^{\s])`)
	mathSubSingleRe  = regexp.MustCompile(`_([^{\s])`)
	mathBraceGroupRe = regexp.MustCompile(`\{([^{}]*)\}`)
	mathAllCmdsRe    = regexp.MustCompile(`\\[a-zA-Z]+`)
)

// latexSymbols maps LaTeX command names (without leading backslash) to their
// Unicode/text equivalents.
var latexSymbols = map[string]string{
	// Greek lowercase
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ",
	"epsilon": "ε", "varepsilon": "ε", "zeta": "ζ", "eta": "η",
	"theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ",
	"lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ",
	"pi": "π", "varpi": "ϖ", "rho": "ρ", "varrho": "ϱ",
	"sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ",
	"phi": "φ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	// Greek uppercase
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ",
	"Xi": "Ξ", "Pi": "Π", "Sigma": "Σ", "Upsilon": "Υ",
	"Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	// Arrows
	"leftarrow": "←", "rightarrow": "→", "uparrow": "↑", "downarrow": "↓",
	"leftrightarrow": "↔", "Leftarrow": "⇐", "Rightarrow": "⇒",
	"Uparrow": "⇑", "Downarrow": "⇓", "Leftrightarrow": "⇔",
	"mapsto": "↦", "to": "→", "gets": "←",
	"nearrow": "↗", "searrow": "↘", "swarrow": "↙", "nwarrow": "↖",
	"longrightarrow": "⟶", "longleftarrow": "⟵", "longleftrightarrow": "⟷",
	"Longrightarrow": "⟹", "Longleftarrow": "⟸", "Longleftrightarrow": "⟺",
	"xrightarrow": "→", "xleftarrow": "←",
	// Relations
	"leq": "≤", "geq": "≥", "neq": "≠", "approx": "≈",
	"equiv": "≡", "sim": "∼", "simeq": "≃", "cong": "≅",
	"propto": "∝", "perp": "⊥", "mid": "∣", "nmid": "∤",
	"ll": "≪", "gg": "≫", "leqq": "≦", "geqq": "≧",
	"prec": "≺", "succ": "≻", "preceq": "⪯", "succeq": "⪰",
	"ne": "≠", "le": "≤", "ge": "≥",
	// Binary operators
	"pm": "±", "mp": "∓", "times": "×", "div": "÷",
	"cdot": "·", "ast": "∗", "star": "⋆", "circ": "∘",
	"bullet": "•", "oplus": "⊕", "ominus": "⊖", "otimes": "⊗",
	"oslash": "⊘", "odot": "⊙", "cap": "∩", "cup": "∪",
	// Set theory
	"subset": "⊂", "supset": "⊃", "subseteq": "⊆", "supseteq": "⊇",
	"nsubseteq": "⊄", "nsupseteq": "⊅",
	"in": "∈", "notin": "∉", "ni": "∋",
	"emptyset": "∅", "varnothing": "∅", "setminus": "∖",
	// Logic
	"land": "∧", "lor": "∨", "lnot": "¬", "neg": "¬",
	"forall": "∀", "exists": "∃", "nexists": "∄",
	"top": "⊤", "bot": "⊥",
	"vdash": "⊢", "models": "⊨", "implies": "⟹", "iff": "⟺",
	// Calculus / analysis
	"infty": "∞", "partial": "∂", "nabla": "∇",
	"int": "∫", "iint": "∬", "iiint": "∭", "oint": "∮",
	"sum": "∑", "prod": "∏", "coprod": "∐",
	"sqrt": "√",
	// Ellipses and misc spacing (spacing → single space or empty)
	"dots": "…", "ldots": "…", "cdots": "⋯", "vdots": "⋮", "ddots": "⋱",
	"quad": " ", "qquad": "  ",
	"thinspace": " ", ",": " ", ";": " ", ":": " ", "!": "",
	// Delimiters
	"langle": "⟨", "rangle": "⟩",
	"lceil": "⌈", "rceil": "⌉", "lfloor": "⌊", "rfloor": "⌋",
	"lvert": "|", "rvert": "|", "lVert": "‖", "rVert": "‖",
	// Misc symbols
	"angle": "∠", "triangle": "△", "square": "□", "diamond": "◇",
	"hbar": "ℏ", "ell": "ℓ", "Re": "ℜ", "Im": "ℑ",
	"wp": "℘", "aleph": "ℵ", "beth": "ℶ",
	"dagger": "†", "ddagger": "‡", "sharp": "♯", "flat": "♭",
	"clubsuit": "♣", "diamondsuit": "♦", "heartsuit": "♥", "spadesuit": "♠",
	// Trig / functions rendered as plain text
	"sin": "sin", "cos": "cos", "tan": "tan", "cot": "cot",
	"sec": "sec", "csc": "csc", "arcsin": "arcsin", "arccos": "arccos",
	"arctan": "arctan", "sinh": "sinh", "cosh": "cosh", "tanh": "tanh",
	"log": "log", "ln": "ln", "exp": "exp", "lg": "lg",
	"lim": "lim", "limsup": "lim sup", "liminf": "lim inf",
	"min": "min", "max": "max", "inf": "inf", "sup": "sup",
	"det": "det", "dim": "dim", "ker": "ker", "deg": "deg",
	"gcd": "gcd", "lcm": "lcm", "Pr": "Pr", "arg": "arg",
	// Accents / modifiers (strip the command, keep argument via brace handling)
	"hat": "", "tilde": "", "bar": "", "vec": "", "dot": "", "ddot": "",
	"overline": "", "underline": "", "widehat": "", "widetilde": "",
	"overrightarrow": "", "overleftarrow": "",
	// Sizing / spacing commands to strip
	"left": "", "right": "", "big": "", "Big": "", "bigg": "", "Bigg": "",
	"mathrm": "", "mathit": "", "mathbf": "", "mathsf": "", "mathtt": "",
	"mathcal": "", "mathscr": "", "mathfrak": "", "mathbb": "",
	"displaystyle": "", "textstyle": "", "scriptstyle": "",
	"boldmath": "", "boldsymbol": "",
	// Misc commands to strip cleanly
	"not": "¬", "bmod": "mod", "pmod": "(mod)",
	"frac": "", "dfrac": "", "tfrac": "", "cfrac": "",
	"over":   "/",
	"choose": "choose",
}

var mathbbSymbols = map[string]string{
	"N": "ℕ", "Z": "ℤ", "Q": "ℚ", "R": "ℝ", "C": "ℂ",
	"P": "ℙ", "F": "𝔽", "H": "ℍ", "k": "𝕜",
}

// preprocessMath replaces $...$ and $$...$$ expressions with placeholders
// before goldmark processes the document, protecting them from goldmark's
// emphasis/escape handling. Code blocks are temporarily shielded to avoid
// matching dollar signs inside them.
func preprocessMath(content string) (string, map[string]mathEntry) {
	mathMap := make(map[string]mathEntry)
	counter := 0
	protected := make(map[string]string)
	pCounter := 0

	// Placeholders use only alphanumerics to avoid triggering goldmark's
	// emphasis parser (double underscores = bold, asterisks = italic, etc.).
	protect := func(m string) string {
		k := fmt.Sprintf("KMCODExxx%dxxxKMCODE", pCounter)
		pCounter++
		protected[k] = m
		return k
	}

	// Shield fenced and inline code blocks.
	content = mathCodeBlockRe.ReplaceAllStringFunc(content, protect)
	content = mathCodeInlineRe.ReplaceAllStringFunc(content, protect)

	// Display math ($$...$$) must be detected before inline ($...$).
	content = mathDisplayRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := mathDisplayRe.FindStringSubmatch(m)
		k := fmt.Sprintf("KMATHxxx%dxxxKMATH", counter)
		counter++
		mathMap[k] = mathEntry{latex: strings.TrimSpace(sub[1]), display: true}
		return k
	})

	// Inline math ($...$): content must start with a non-space character.
	content = mathInlineRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := mathInlineRe.FindStringSubmatch(m)
		k := fmt.Sprintf("KMATHxxx%dxxxKMATH", counter)
		counter++
		mathMap[k] = mathEntry{latex: sub[1], display: false}
		return k
	})

	// Restore code blocks.
	for k, v := range protected {
		content = strings.ReplaceAll(content, k, v)
	}
	return content, mathMap
}

// restoreMath replaces math placeholders in goldmark's HTML output with
// rendered math HTML.
func restoreMath(html string, mathMap map[string]mathEntry) string {
	for key, entry := range mathMap {
		rendered := renderMathExpression(entry.latex, entry.display)
		if entry.display {
			// Goldmark wraps a bare line of text in <p>; unwrap it so the
			// block <div> isn't nested inside a <p> (invalid HTML).
			html = strings.ReplaceAll(html, "<p>"+key+"</p>\n", rendered+"\n")
			html = strings.ReplaceAll(html, "<p>"+key+"</p>", rendered)
		}
		// Also handle goldmark wrapping placeholder in <em> or <strong> when
		// the placeholder text happens to coincide with an emphasis pattern
		// (should not occur with the current alphanumeric placeholder format,
		// but guard defensively).
		html = strings.ReplaceAll(html, "<em>"+key+"</em>", rendered)
		html = strings.ReplaceAll(html, "<strong>"+key+"</strong>", rendered)
		html = strings.ReplaceAll(html, key, rendered)
	}
	return html
}

// renderMathExpression converts a LaTeX math string to an HTML fragment.
// Common commands are converted to Unicode; super/subscripts use <sup>/<sub>.
func renderMathExpression(latex string, display bool) string {
	// HTML-escape raw content so any literal <, >, & in the LaTeX become
	// safe entities. Our generated <sup>/<sub> tags are added after this.
	latex = strings.ReplaceAll(latex, "&", "&amp;")
	latex = strings.ReplaceAll(latex, "<", "&lt;")
	latex = strings.ReplaceAll(latex, ">", "&gt;")

	// \frac{a}{b} → superscript/subscript fraction
	latex = mathFracRe.ReplaceAllString(latex, "<sup>$1</sup>⁄<sub>$2</sub>")

	// \text{...} and font variants → bare text
	latex = mathTextCmdRe.ReplaceAllString(latex, "$1")

	// \mathbb{X} → double-struck letter
	latex = mathMathbbRe.ReplaceAllStringFunc(latex, func(m string) string {
		sub := mathMathbbRe.FindStringSubmatch(m)
		if s, ok := mathbbSymbols[sub[1]]; ok {
			return s
		}
		return sub[1]
	})

	// Other font commands (\mathcal, \mathbf, etc.) → strip command, keep content
	latex = mathFontCmdRe.ReplaceAllString(latex, "$1")

	// ^{...} → <sup>...</sup>  (loop handles simple nesting)
	for mathSupBracesRe.MatchString(latex) {
		latex = mathSupBracesRe.ReplaceAllString(latex, "<sup>$1</sup>")
	}

	// _{...} → <sub>...</sub>
	for mathSubBracesRe.MatchString(latex) {
		latex = mathSubBracesRe.ReplaceAllString(latex, "<sub>$1</sub>")
	}

	// Apply symbol table to all remaining \commands.
	latex = mathAllCmdsRe.ReplaceAllStringFunc(latex, func(cmd string) string {
		name := cmd[1:] // strip leading backslash
		if sym, ok := latexSymbols[name]; ok {
			return sym
		}
		return "" // strip unknown commands silently
	})

	// Single-character super/subscripts (after brace and symbol processing).
	latex = mathSupSingleRe.ReplaceAllString(latex, "<sup>$1</sup>")
	latex = mathSubSingleRe.ReplaceAllString(latex, "<sub>$1</sub>")

	// Strip any remaining bare braces.
	for mathBraceGroupRe.MatchString(latex) {
		latex = mathBraceGroupRe.ReplaceAllString(latex, "$1")
	}
	latex = strings.ReplaceAll(latex, "{", "")
	latex = strings.ReplaceAll(latex, "}", "")

	if display {
		return `<div class="math-display">` + latex + `</div>`
	}
	return `<span class="math-inline">` + latex + `</span>`
}
