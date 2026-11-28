package topolab

// The SDK says what the Advanced tier is (columns marked Advanced on the
// dataset page) and never which plans hold it: who gets Advanced changes with
// the plans and the Advanced add-on, and the dataset page is where that is
// stated. A sentence that names the tier names no plan.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	tierWord     = regexp.MustCompile(`(?i)\badvanced\b`)
	planWord     = regexp.MustCompile(`\b(Hobbyist|Lite|Team|Enterprise)\b`)
	sentenceEnd  = regexp.MustCompile(`[.!?](\s+|$)|\n\s*\n`)
	commentStart = regexp.MustCompile(`(?m)^\s*//\s?`)
)

func TestTierSentencesNameNoPlan(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	docs, _ := filepath.Glob("docs/*.md")
	files = append(append(files, docs...), "README.md")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := commentStart.ReplaceAllString(string(b), "")
		for _, s := range sentenceEnd.Split(text, -1) {
			s = strings.Join(strings.Fields(s), " ")
			if tierWord.MatchString(s) && planWord.MatchString(s) {
				t.Errorf("%s: a sentence about the Advanced tier names a plan: %q", f, s)
			}
		}
	}
}
