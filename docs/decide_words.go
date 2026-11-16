package docs

import (
	"strings"

	"github.com/1broseidon/ketch/decide"
)

// Decisions (design/docs-evidence.md, Final shape; the v5.1 prototype):
// every question asked of a decision provider, its wording, and the rules
// that read the answers. The wording is the prototype's, word for word; the
// cut-offs are the provider's (decide.Cutoffs), never constants here.

// The questions' names and the shared input's field names.
const (
	qDocumentation = "documentation"
	qMakers        = "makers"
	qNamesRelease  = "names_release"
	qOtherRelease  = "other_release"
	qKind          = "kind"
)

// makersTrue and makersFalse say when a page is the makers' own.
const (
	makersTrue  = "The page is on the project's or vendor's own documentation site, website or source repository."
	makersFalse = "The page is written by someone else: a blog, tutorial site, Q&A answer, forum, news article or a copy of the documentation hosted elsewhere."
)

// kindOptions are the kinds of page a page or a search result can be.
var kindOptions = []decide.Option{
	{Name: "reference_documentation", Description: "Reference documentation: an API, command, configuration or language reference, a manual page or a specification."},
	{Name: "guide_or_tutorial", Description: "A guide or tutorial in a documentation set that walks through how to do something."},
	{Name: "blog_post_or_article", Description: "A blog post, news item or article written by a person or company."},
	{Name: "forum_or_qa_thread", Description: "A forum thread or a question-and-answer page such as Stack Overflow."},
	{Name: "issue_or_discussion", Description: "An issue, pull request or discussion thread in a code hosting site."},
	{Name: "source_code", Description: "A source code file or a repository file listing."},
	{Name: "marketing_or_landing_page", Description: "A marketing, product or landing page."},
	{Name: "other", Description: "Any other kind of page."},
}

// pageQuestions are the page pass: one request per page read, the page in
// the shared input.
var pageQuestions = []decide.Question{
	{
		Name: qDocumentation, Kind: decide.YesNo,
		Instructions: "Is `page` documentation for software: a reference page, manual, specification or guide to how the software works?",
		TrueMeans:    "The page explains how the software works, such as an API or configuration reference, a manual, a specification or a user guide.",
		FalseMeans:   "The page is a blog post or article, a forum or Q&A thread, an issue or discussion, a marketing page, or a page with almost no content.",
	},
	{
		Name: qMakers, Kind: decide.YesNo,
		Instructions: "Is `page` published by the makers of the software that `docs_question` asks about?",
		TrueMeans:    makersTrue, FalseMeans: makersFalse,
	},
	{
		Name: qNamesRelease, Kind: decide.YesNo,
		Instructions: "Does `docs_question` name a specific version or release of the software it asks about?",
		TrueMeans:    `The question gives a version number or a named release, such as "3.12", "v5", "version 2" or "the 2024 edition".`,
		FalseMeans:   "The question names the software without any version or release.",
	},
	{
		Name: qOtherRelease, Kind: decide.YesNo,
		Instructions: "Does `page` document a different version or release than the one `docs_question` names?",
		TrueMeans:    "The page's title, address or text shows a version or release other than the one the question names.",
		FalseMeans:   "The page shows the version the question names, or shows no version at all.",
	},
	{Name: qKind, Kind: decide.Choice, Instructions: "What kind of page is `page`?", Options: kindOptions},
}

// The other results' questions: about each search result not read, the
// result in the question.
const (
	otherMakersAsk = "Is `search_result` published by the makers of the software that `docs_question` asks about?"
	otherKindAsk   = "What kind of page is `search_result`?"
)

// helpsAsk, helpsTrue and helpsFalse are the passage pass's one question,
// asked of every passage of a page, the passage in the question.
const (
	helpsAsk   = "Does `passage` state something that helps answer `docs_question`?"
	helpsTrue  = "The passage states a fact, behaviour, setting, default, rule, limit, version note or example that a correct answer to the question would use."
	helpsFalse = "The passage is about something else, or mentions the topic without stating anything the answer needs, such as navigation, an introduction or an unrelated option."
)

// The limits of what is sent and read.
const (
	// decideReadTop is how many search results are read; those after
	// them are the other results.
	decideReadTop = 5
	// stubWords: a page read with fewer words is almost empty.
	stubWords = 150
	// outlineMax is how many headings the page pass sends.
	outlineMax = 60
	// pageTextChars is the page text the page pass sends, from the top,
	// cut at a line: about 8,000 tokens at four characters a token.
	pageTextChars = 8000 * 4
	// passageTokens: consecutive pieces of one section are one passage up
	// to this size.
	passageTokens = 300
	// passageSendChars cuts the text of a passage sent (only one long
	// block reaches it).
	passageSendChars = 6000
	// snippetChars cuts a search result's snippet sent.
	snippetChars = 600
	// firstPassagePages is how many readable pages get the passage pass
	// together with the page pass.
	firstPassagePages = 3
	// topRanks: a tier A page displaces a better-ranked tier B page only
	// from these search ranks.
	topRanks = 3
	// addMinTokens: with more than this left after the primary page,
	// qualifying passages of other pages are added.
	addMinTokens = 200
	// lowerTokens is the most passages of lower-tier documentation pages
	// add together.
	lowerTokens = 1000
	// clipMinTokens: a top passage larger than the budget is printed cut
	// when at least this much is left.
	clipMinTokens = 50
	// shingleWords is a shingle's length for the repeat rules.
	shingleWords = 8
	// dupShare: a passage this much of whose shingles are printed already
	// is skipped; a page this much of whose text is on the page read is
	// never suggested.
	dupShare = 0.7
)

// The source labels and tiers.
const (
	labelOfficialDocs   = "official docs"
	labelDocs           = "docs"
	labelThirdPartyDocs = "third-party docs"
	labelOtherRelease   = "other release"
	labelUnlabelled     = "unlabelled"
	tierA, tierB, tierC = "A", "B", "C"
)

// kindWords are the kinds of page in plain words; officialKindWords the
// same after "official".
var (
	kindWords = map[string]string{
		"reference_documentation": "reference page", "guide_or_tutorial": "guide", "blog_post_or_article": "article",
		"forum_or_qa_thread": "forum thread", "issue_or_discussion": "discussion", "source_code": "source code",
		"marketing_or_landing_page": "marketing page", "other": "other",
	}
	officialKindWords = map[string]string{
		"reference_documentation": "reference page", "guide_or_tutorial": "guide", "blog_post_or_article": "article",
		"forum_or_qa_thread": "forum thread", "issue_or_discussion": "discussion", "source_code": "source code",
		"marketing_or_landing_page": "marketing page", "other": "page",
	}
	tierOf   = map[string]string{labelOfficialDocs: tierA, labelDocs: tierB, labelThirdPartyDocs: tierB}
	tierBand = map[string]string{tierA: "high", tierB: "medium", tierC: "low"}
)

// The confidence words, lowest first.
const (
	confLow    = "low"
	confMedium = "medium"
	confHigh   = "high"
)

// The passed_over reasons.
const (
	passedSame     = "same page as the one read"
	passedRelease  = "documents another release"
	passedNothing  = "nothing on the question"
	passedAlso     = "also on the question, not printed"
	passedWeaker   = "weaker source than the page read"
	passedNeedless = "not needed"
	passedStub     = "almost empty"
	passedNoDoc    = "not a document by its address"
)

// The notes a decided answer can carry.
const (
	noteNoPassage   = "no page read has a passage on the question; printed the first readable result, as the search ranks it"
	noteOtherRel    = "the question names a release; the page read documents another one"
	noteNotOfficial = "no result read is documentation from the makers or a reference; the page read is "
)

// cutoffs reads a provider's probabilities.
type cutoffs decide.Cutoffs

func (c cutoffs) hi(x float64) bool { return x >= c.High }
func (c cutoffs) lo(x float64) bool { return x <= c.Low }

// sourceLabel is a page's label from the page pass: documentation high is
// official docs (makers at or above Official), third-party docs (makers
// low) or docs; otherwise official <kind> (makers at or above Official)
// or the kind in plain words.
func (c cutoffs) sourceLabel(documentation, makers float64, kind string) string {
	if c.hi(documentation) {
		switch {
		case makers >= c.Official:
			return labelOfficialDocs
		case c.lo(makers):
			return labelThirdPartyDocs
		}
		return labelDocs
	}
	if makers >= c.Official {
		return "official " + officialKindWords[kind]
	}
	return kindWords[kind]
}

// otherLabel is a search result's label from makers and kind alone: a
// reference page or guide stands in for documentation high.
func (c cutoffs) otherLabel(makers float64, kind string) string {
	doc := 0.0
	if kind == "reference_documentation" || kind == "guide_or_tutorial" {
		doc = 1
	}
	return c.sourceLabel(doc, makers, kind)
}

// tier is a label's tier, A official docs, B docs and third-party docs, C
// the rest; a page documenting another release than the one the question
// names drops one tier and says so in its label.
func (c cutoffs) tier(label string, releaseNamed bool, otherRelease float64) (string, string) {
	t, ok := tierOf[label]
	if !ok {
		t = tierC
	}
	if releaseNamed && c.hi(otherRelease) {
		return map[string]string{tierA: tierB, tierB: tierC, tierC: tierC}[t], label + ", " + labelOtherRelease
	}
	return t, label
}

// context is a page's context from its passages: high when one is high,
// low when none is above Low, else medium.
func (c cutoffs) context(probs []float64) string {
	above := false
	for _, p := range probs {
		if c.hi(p) {
			return confHigh
		}
		above = above || p > c.Low
	}
	if !above {
		return confLow
	}
	return confMedium
}

// band is a passage's probability in words.
func (c cutoffs) band(p float64) string {
	switch {
	case c.hi(p):
		return confHigh
	case p > c.Low:
		return confMedium
	}
	return confLow
}

// lower is the lowest of confidence words.
func lower(words ...string) string {
	rank := map[string]int{confLow: 0, confMedium: 1, confHigh: 2}
	out := confHigh
	for _, w := range words {
		if rank[w] < rank[out] {
			out = w
		}
	}
	return out
}

// article is "a" or "an" before word.
func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an "
	}
	return "a "
}
