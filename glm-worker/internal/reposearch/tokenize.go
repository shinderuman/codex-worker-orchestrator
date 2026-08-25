package reposearch

import (
	"strings"
	"unicode"
)

const tokenizerVersion = 1

func tokenize(text string) []string {
	var tokens []string
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		if isCJK(run[0]) {
			tokens = append(tokens, cjkGrams(run)...)
		} else {
			tokens = append(tokens, strings.ToLower(string(run)))
		}
		run = run[:0]
	}
	for _, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(run) > 0 && isCJK(r) != isCJK(run[len(run)-1]) {
			flush()
		}
		run = append(run, r)
	}
	flush()
	return tokens
}

var katakanaProlongedSoundMark = &unicode.RangeTable{
	R16: []unicode.Range16{{Lo: 0x30FC, Hi: 0x30FC, Stride: 1}},
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) ||
		unicode.Is(katakanaProlongedSoundMark, r)
}

func cjkGrams(run []rune) []string {
	if len(run) == 1 {
		return []string{string(run)}
	}
	grams := make([]string, 0, len(run)-1)
	for i := 0; i+1 < len(run); i++ {
		grams = append(grams, string(run[i:i+2]))
	}
	return grams
}

func termFrequencies(tokens []string) map[string]int {
	if len(tokens) == 0 {
		return nil
	}
	counts := make(map[string]int, len(tokens))
	for _, token := range tokens {
		counts[token]++
	}
	return counts
}
