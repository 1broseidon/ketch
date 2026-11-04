package docs

import (
	"unicode"
	"unicode/utf8"
)

// Token estimates for the local backend's budgets. Bytes divided by four
// is a fair average for English prose but undercounts code, tables and
// symbols, which tokenize densely, and overcounts long common words. The
// estimate here splits text the way o200k's pre-tokenizer does (words with
// a leading space or punctuation character, up to three digits, runs of
// punctuation, whitespace) and charges each piece what o200k charges such a
// piece on average: about one token for a word of up to eight letters and
// a quarter more per letter past that, one for a run of punctuation and a
// half per character after its first (a repeat, as in "-----", much
// less), one for each non-ASCII symbol, and so on. The weights were fitted
// by least squares against o200k_base on 2,651 rendered local results
// (section families and snippets from 42 indexed libraries) using half of
// the libraries, and raised 2%. On the other half a result's estimate is
// off by 4.3% on average (95th percentile 9.6%; bytes/4: 9.2% and 23%)
// and a five-result answer's by 3.5% (worst 9% under). ADR-0007,
// amendment 3.

// tokenWeights are the per-piece charges, in hundredths of a token.
var tokenWeights = struct {
	word, longWord, letter, digits, punct, punctChar, repeat, space, symbol int
}{word: 106, longWord: 89, letter: 26, digits: 145, punct: 92, punctChar: 53, repeat: 16, space: 101, symbol: 97}

// tokenWordLetters is the longest word charged as one piece; past it,
// each letter adds tokenWeights.letter.
const tokenWordLetters = 8

// estimateTokens estimates s's o200k token count.
func estimateTokens(s string) int {
	f := tokenPieces(s)
	w := tokenWeights
	hundredths := f.words*w.word + f.longWords*w.longWord + f.extraLetters*w.letter + f.digitRuns*w.digits +
		f.puncts*w.punct + f.extraPunct*w.punctChar + f.repeats*w.repeat + f.spaces*w.space + f.symbols*w.symbol
	return (hundredths + 99) / 100
}

// pieceCounts are the pre-tokenizer's pieces of a text, by kind.
type pieceCounts struct {
	words        int // letter runs of at most tokenWordLetters letters
	longWords    int // longer ones
	extraLetters int // letters past tokenWordLetters in long words
	digitRuns    int // runs of up to three digits
	puncts       int // runs of punctuation and symbols
	extraPunct   int // characters past the first in those runs, not repeats or symbols
	repeats      int // characters in those runs that repeat the one before
	spaces       int // whitespace runs (indentation, line breaks)
	symbols      int // non-ASCII characters that are not letters (emoji, arrows, box drawing)
}

// tokenPieces splits s as o200k's pre-tokenizer does, approximately: a
// word is an optional leading character that is not a letter, digit or
// line break, then capitals and then lower-case letters (camelCase is two
// words); digits go in threes; a punctuation run takes an optional
// leading space and trailing line breaks; whitespace runs end before the
// space that leads the next piece.
func tokenPieces(s string) pieceCounts {
	var f pieceCounts
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r >= utf8.RuneSelf && !unicode.IsLetter(r) && !unicode.IsSpace(r) && !unicode.IsDigit(r) {
			f.symbols++
		}
		switch {
		case wordAt(s, i, r, n):
			i = f.word(s, i, r, n)
		case unicode.IsDigit(r):
			i = f.digits(s, i)
		case unicode.IsSpace(r) && (r != ' ' || !punctAt(s, i+n)):
			i = scanSpace(s, i)
			f.spaces++
		default:
			i = f.punct(s, i, r, n)
		}
	}
	return f
}

// wordAt reports a word at i: a letter, or a character that is not a
// letter, digit or line break followed by one.
func wordAt(s string, i int, r rune, n int) bool {
	if unicode.IsLetter(r) {
		return true
	}
	if unicode.IsDigit(r) || r == '\n' || r == '\r' {
		return false
	}
	r2, _ := utf8.DecodeRuneInString(s[i+n:])
	return i+n < len(s) && unicode.IsLetter(r2)
}

// word counts the word at i (wordAt) and returns its end.
func (f *pieceCounts) word(s string, i int, r rune, n int) int {
	if !unicode.IsLetter(r) {
		i += n
	}
	end, letters := scanWord(s, i)
	if letters > tokenWordLetters {
		f.longWords++
		f.extraLetters += letters - tokenWordLetters
	} else {
		f.words++
	}
	return end
}

// digits counts the run of up to three digits at i and returns its end.
func (f *pieceCounts) digits(s string, i int) int {
	for k := 0; i < len(s) && k < 3; k++ {
		d, dn := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsDigit(d) {
			break
		}
		i += dn
	}
	f.digitRuns++
	return i
}

// punct counts the punctuation run at i (with a leading space and the
// line breaks and slashes after it) and returns its end.
func (f *pieceCounts) punct(s string, i int, r rune, n int) int {
	j, chars := i, 0
	if r == ' ' {
		j += n
	}
	prev := rune(-1)
	for j < len(s) {
		p, pn := utf8.DecodeRuneInString(s[j:])
		if unicode.IsSpace(p) || unicode.IsLetter(p) || unicode.IsDigit(p) {
			break
		}
		switch {
		case p == prev:
			f.repeats++ // "-----", "═════": long merges
		case j > i && p >= utf8.RuneSelf:
			f.symbols++
		case chars > 0:
			f.extraPunct++
		}
		prev = p
		j += pn
		chars++
	}
	for j < len(s) && (s[j] == '\n' || s[j] == '\r' || s[j] == '/') {
		j++
	}
	if chars == 0 { // a lone space before a digit
		f.spaces++
		return i + n
	}
	f.puncts++
	return j
}

// punctAt reports a punctuation or symbol character at i.
func punctAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return !unicode.IsSpace(r) && !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// scanWord reads capitals then lower-case letters (and letters of no
// case) from i: the end and the letter count.
func scanWord(s string, i int) (int, int) {
	letters := 0
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsUpper(r) && !unicode.IsTitle(r) {
			break
		}
		i += n
		letters++
	}
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLetter(r) && !unicode.IsMark(r) || unicode.IsUpper(r) || unicode.IsTitle(r) {
			break
		}
		i += n
		letters++
	}
	return i, letters
}

// scanSpace reads a whitespace run from i: through its last line break
// when it holds one, else up to the space that leads the next piece.
func scanSpace(s string, i int) int {
	j, lastNL := i, -1
	for j < len(s) {
		r, n := utf8.DecodeRuneInString(s[j:])
		if !unicode.IsSpace(r) {
			break
		}
		if r == '\n' || r == '\r' {
			lastNL = j + n
		}
		j += n
	}
	if lastNL > 0 {
		return lastNL
	}
	if j < len(s) && j-i > 1 && s[j-1] == ' ' {
		return j - 1 // the last space leads the next piece
	}
	return j
}
