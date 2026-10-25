package main

// Guess the UI language from what the user actually types to their agents.
// Port of src/lang.js. Returns a code from supportedLangs, or "" when there
// isn't enough text.

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var supportedLangs = []string{"en", "zh-TW", "zh-CN", "ja", "ko", "es", "fr", "de", "pt"}

// Characters that differ between Traditional and Simplified Chinese.
const (
	tradChars = "們這個說會來時為麼發對後還開過們國學實現點頭樣讓進與東車問題當經動種長門關應這給從點"
	simpChars = "们这个说会来时为么发对后还开过们国学实现点头样让进与东车问题当经动种长门关应这给从点"
)

var tradSet, simpSet = func() (map[rune]bool, map[rune]bool) {
	tr, sp := []rune(tradChars), []rune(simpChars)
	ts, ss := map[rune]bool{}, map[rune]bool{}
	for i := range tr {
		if tr[i] != sp[i] {
			ts[tr[i]] = true
			ss[sp[i]] = true
		}
	}
	return ts, ss
}()

// Frequent short words, to tell Latin-script languages apart. Order matters:
// ties resolve to the earlier language, as Object.entries did in the JS.
var stopLangs = []string{"en", "es", "fr", "de", "pt"}
var stopWords = map[string]map[string]bool{
	"en": setOf("the", "and", "is", "to", "it", "this", "that", "with", "for", "you", "can", "please", "what", "add", "fix", "make"),
	"es": setOf("el", "la", "que", "los", "las", "por", "para", "una", "con", "esto", "puedes", "hacer", "está", "pero"),
	"fr": setOf("le", "les", "est", "et", "des", "une", "pour", "avec", "pas", "que", "peux", "faire", "cette", "dans"),
	"de": setOf("der", "die", "das", "und", "ist", "nicht", "mit", "ein", "eine", "bitte", "kannst", "auch", "für", "auf"),
	"pt": setOf("não", "que", "uma", "com", "para", "isso", "você", "pode", "fazer", "está", "mas", "os", "as", "do", "da"),
}

func setOf(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var reWordSplit = regexp.MustCompile(`[^a-zà-ÿ]+`)

func guessLanguage(texts []string, minWeight int) string {
	var han, kana, hangul, latin, trad, simp int
	var words []string
	for _, t := range texts {
		if t == "" {
			continue
		}
		for _, c := range t {
			switch {
			case c >= 0x3040 && c <= 0x30ff:
				kana++
			case c >= 0xac00 && c <= 0xd7af:
				hangul++
			case c >= 0x4e00 && c <= 0x9fff:
				han++
				if tradSet[c] {
					trad++
				} else if simpSet[c] {
					simp++
				}
			case (c >= 0x41 && c <= 0x5a) || (c >= 0x61 && c <= 0x7a) || (c >= 0xc0 && c <= 0x24f):
				latin++
			}
		}
		// Skip code-ish tokens (paths, flags, identifiers) for word counts.
		for _, w := range reWordSplit.Split(strings.ToLower(t), -1) {
			if n := utf8.RuneCountInString(w); n > 1 && n < 12 {
				words = append(words, w)
			}
		}
	}
	// A CJK character carries roughly a word's worth of meaning; weigh it up
	// so English identifiers sprinkled in Chinese prompts don't win.
	cjk := han + kana + hangul
	if cjk*4+latin < minWeight {
		return ""
	}
	if cjk*4 > latin {
		if float64(kana) > float64(cjk)*0.1 {
			return "ja"
		}
		if float64(hangul) > float64(cjk)*0.3 {
			return "ko"
		}
		if simp > trad {
			return "zh-CN"
		}
		return "zh-TW"
	}
	best, bestN := "", -1
	for _, lang := range stopLangs {
		n := 0
		for _, w := range words {
			if stopWords[lang][w] {
				n++
			}
		}
		if n > bestN {
			best, bestN = lang, n
		}
	}
	if bestN >= 3 {
		return best
	}
	return "en"
}

// Each distinct prompt gets one vote, so a scheduled job that sends the
// same English prompt fifty times doesn't outvote what the person types.
func guessFromPrompts(prompts []string) string {
	votes := map[string]int{}
	var order []string
	seen := map[string]bool{}
	for _, p := range prompts {
		key := strings.TrimSpace(p)
		if r := []rune(key); len(r) > 80 {
			key = string(r[:80])
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if lang := guessLanguage([]string{p}, 6); lang != "" {
			if _, ok := votes[lang]; !ok {
				order = append(order, lang)
			}
			votes[lang]++
		}
	}
	best, bestN := "", -1
	for _, lang := range order {
		if votes[lang] > bestN {
			best, bestN = lang, votes[lang]
		}
	}
	if bestN >= 2 {
		return best
	}
	return ""
}
