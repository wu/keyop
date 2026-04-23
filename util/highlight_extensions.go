package util

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// customKeywordList defines extra keywords to add to a language's highlighting.
type customKeywordList struct {
	words     []string // Words to match
	tokenType chroma.TokenType
}

// customKeywords maps Chroma language names to additional keywords to highlight.
// Language name lookup is case-insensitive (use "bash" or "Bash", both work).
// The actual lexer name is resolved internally for rule matching.
var customKeywords = map[string][]customKeywordList{
	// Bash/shell: highlight sudo and doas as builtins (same style as echo, cd, alias, etc)
	"bash": {
		{words: []string{
			"apt",
			"apt-cache",
			"apt-get",
			"bash",
			"brew",
			"caffeinate",
			"cat",
			"cd",
			"chmod",
			"chown",
			"claude",
			"cloc",
			"cp",
			"curl",
			"dd",
			"df",
			"diff",
			"dmesg",
			"docker",
			"dpkg",
			"dpkg-reconfigure",
			"echo",
			"exit",
			"fdisk",
			"fswatch",
			"git",
			"go",
			"goaccess",
			"grep",
			"groupadd",
			"install",
			"ifconfig",
			"ip",
			"journalctl",
			"json_pp",
			"keyop",
			"ln",
			"ls",
			"lsb_release",
			"lsblk",
			"make",
			"makemkvcon",
			"mkdir",
			"mount",
			"mv",
			"nmcli",
			"npm",
			"ollama",
			"open",
			"openssl",
			"perl",
			"ping",
			"pmset",
			"rfkill",
			"rm",
			"rsync",
			"scp",
			"sed",
			"set",
			"sha256sum",
			"sqlite3",
			"ssh",
			"ssh-keygen",
			"su",
			"sudo",
			"systemctl",
			"tail",
			"tee",
			"tmux",
			"touch",
			"ufw",
			"umount",
			"usermod",
			"uuid",
			"vi",
			"wpa_cli",
			"wpa_passphrase",
			"xz",
		}, tokenType: chroma.NameBuiltin},
	},
}

// init registers all custom lexer extensions at startup.
// This runs before any RenderMarkdown calls, so all code blocks benefit automatically.
func init() {
	for langName, keywords := range customKeywords {
		extendLexerWithKeywords(langName, keywords)
	}
}

// extendLexerWithKeywords extends an existing Chroma lexer by adding new keyword rules.
// It clones the existing lexer's rules, adds new rules for the custom keywords,
// and registers the extended lexer under the same language name.
func extendLexerWithKeywords(langName string, keywords []customKeywordList) {
	// Get the existing lexer for this language
	base := lexers.Get(langName)
	if base == nil {
		// Language not found, silently skip
		return
	}

	// Try to get the underlying RegexLexer so we can access its rules
	regexLexer, ok := base.(*chroma.RegexLexer)
	if !ok {
		// Not a regex lexer, can't extend rules; silently skip
		return
	}

	// Get a copy of the current rules (this triggers lazy initialization)
	rules, _ := regexLexer.Rules()
	if len(rules) == 0 {
		return
	}

	// Clone the rules so we can modify them
	newRules := rules.Clone()

	// Add new keyword rules to the "basic" state (where most tokenization happens)
	// Use the actual lexer's Config.Name to be independent of how user named it
	if base.Config().Name == "Bash" && len(newRules["basic"]) > 0 {
		// Build a regex pattern that matches any of the custom words as a complete word
		for _, kwList := range keywords {
			if len(kwList.words) == 0 {
				continue
			}
			// Create a rule that matches these words at word boundaries on both sides
			// Words() with both prefix and suffix ensures proper word boundary matching
			pattern := chroma.Words(`\b`, `\b`, kwList.words...)
			newRule := chroma.Rule{
				Pattern: pattern,
				Type:    kwList.tokenType,
			}
			// Insert at the beginning of the basic state so it matches before less specific rules
			newRules["basic"] = append([]chroma.Rule{newRule}, newRules["basic"]...)
		}
	}

	// Create a new lexer with the extended rules, using the original config
	extendedLexer := chroma.MustNewLexer(base.Config(), func() chroma.Rules { return newRules })

	// Register the extended lexer under the same name, replacing the original
	lexers.Register(extendedLexer)
}
