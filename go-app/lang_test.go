package main

import "testing"

func TestGuessLanguageScripts(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"請幫我修正這個錯誤並說明原因", "zh-TW"},
		{"请帮我修复这个错误并说明原因", "zh-CN"},
		{"このバグを直してください", "ja"},
		{"이 버그를 고쳐주세요", "ko"},
		{"please fix the bug and make it work with the tests", "en"},
		{"por favor arregla el error para que los tests pasen con esto", "es"},
		{"peux-tu corriger le bug pour que les tests passent avec cette version", "fr"},
		{"bitte behebe den fehler damit die tests mit der neuen version laufen", "de"},
		{"você pode corrigir o erro para que os testes passem com isso", "pt"},
		{"refactor parseArgs", "en"}, // Latin with no stop words falls back to en
	}
	for _, c := range cases {
		if got := guessLanguage([]string{c.text}, 6); got != c.want {
			t.Errorf("%q: got %q, want %q", c.text, got, c.want)
		}
	}
}

func TestGuessLanguageTooShort(t *testing.T) {
	if got := guessLanguage([]string{"hi"}, 6); got != "" {
		t.Errorf("two Latin letters are below the weight floor, got %q", got)
	}
	if got := guessLanguage([]string{"好"}, 6); got != "" {
		t.Errorf("one CJK char (weight 4) is below 6, got %q", got)
	}
	if got := guessLanguage([]string{"好的"}, 6); got != "zh-TW" {
		t.Errorf("two CJK chars (weight 8) pass, got %q", got)
	}
	if got := guessLanguage(nil, 6); got != "" {
		t.Errorf("no input, got %q", got)
	}
}

func TestGuessLanguageCJKOutweighsIdentifiers(t *testing.T) {
	// English identifiers sprinkled into a Chinese prompt must not flip it to en.
	got := guessLanguage([]string{"把 parseArgs 和 NewWatcher 這兩個函式改成回傳錯誤"}, 6)
	if got != "zh-TW" {
		t.Errorf("got %q", got)
	}
}

func TestGuessFromPromptsVotes(t *testing.T) {
	if got := guessFromPrompts([]string{"請幫我修正這個錯誤"}); got != "" {
		t.Errorf("one vote is not enough, got %q", got)
	}
	if got := guessFromPrompts([]string{"請幫我修正這個錯誤", "然後把測試補上去"}); got != "zh-TW" {
		t.Errorf("two zh votes, got %q", got)
	}
	// The same prompt repeated is one vote: two distinct English prompts win.
	same := "請幫我修正這個錯誤並說明原因"
	got := guessFromPrompts([]string{same, same, same, same, same,
		"please fix the bug and make it work with the tests",
		"can you add the missing test for this and that"})
	if got != "en" {
		t.Errorf("dedupe: got %q", got)
	}
	if got := guessFromPrompts([]string{"", "   ", "hi"}); got != "" {
		t.Errorf("blank and too-short prompts, got %q", got)
	}
}

func TestGuessFromPromptsTieKeepsFirst(t *testing.T) {
	got := guessFromPrompts([]string{
		"請幫我修正這個錯誤並說明原因", "然後把測試補上去好嗎",
		"please fix the bug and make it work with the tests", "can you add the missing test for this and that",
	})
	if got != "zh-TW" {
		t.Errorf("tie resolves to the language seen first, got %q", got)
	}
}
