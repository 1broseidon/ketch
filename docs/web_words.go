package docs

import "strings"

// Common words (decision-15). Registries hold a package for nearly every
// English word (npm's set, parse and add; crates.io's best), so a common
// word in a question is far more often the word than a library's name.
// When a reading's name is one (isCommonWord), registry existence and a
// verified site are not enough: the library must also be in common use
// and win over every other reading (commonWordBar, resolveQuery). Popular
// libraries named by common words (click, requests, express, rich) clear
// that bar; the namesake of a word nobody asked about does not.
//
// The list is general vocabulary: frequent English verbs, nouns,
// adjectives and adverbs, and the words programming questions are made
// of. It names no library on purpose; a library that is also a common
// word is weighed, not listed.

// commonWords is the vocabulary isCommonWord knows.
var commonWords = wordSet(`
about above accept access account act action active actual add address admin advanced after again against age
agent agree ahead aim alert align all allow alone along already also alter always amount analyze anchor and
angle animate animation another answer any anyhow anything anyway apart append apply approach area argue
argument around array arrow art article ask assert assign async at attach attempt attr attribute audio auth
author auto available avoid await away axis back background backup bad badge balance bar base basic batch be
bear beat become before begin behavior behaviour behind being bell below best better between big bind bit
black blank block blue board body bold book boolean boot border both bottom bound box branch brand break
bridge brief bright bring broad broken browse browser buffer bug build builder bulk bundle burn bus business
busy button buy by byte cache calculate calendar call callback camera can cancel canvas cap capital capture
card care carry case cast catch cause cell center central chain chance change channel chapter char character
charge chart chat cheap check child choice choose chunk circle claim class clean clear click client clip
clock clone close cloud cluster code coerce cold collect collection color colour column combine come command
comment commit common compare compile complete component compose compute concat condition config configure
confirm conflict connect connection console constant construct consume contact contain container content
context continue contract control convert cookie copy core correct cost count counter country course cover
crash create credential cron cross crypto current cursor curve custom cut cycle daemon daily dark dash data
database date day dead deal debug decide declare decode decorate decorator deep default defer define delay
delete deliver demo deny deploy depth derive describe design destroy detail detect develop device dialog
diff different direct directory disable disk display distance dive divide do doc document dom domain done
door double down download draft drag draw drive driver drop due dump duplicate during dynamic each early
easy echo edge edit effect element else email embed emit empty enable encode end endpoint engine enter
entity entry enum env environment equal error escape event every exact example except exception exec
execute exist exit expand expect expire explain export expose express extend extension extra extract face
fact factory fail fake fall false fast feature feed fetch field figure file fill filter final find fine
finish fire first fix fixture flag flat float flow flush focus fold folder follow font force form format
forward frame free fresh from front full fun function future game gap gate general generate generic get
give glob global go goal good graph great green grid group grow guard guide half handle handler happen hard
hash have head header heading health heap height hello help hidden hide high highlight history hit hold
home hook host hot hour how idea identity idle if ignore image import improve include increase indent index
info inject inline inner input insert inside install instance int integer interface internal interval into
invalid invoke issue item join json jump just keep key kind label language large last late launch layer
layout lazy lead leak learn leave left legacy length less let level library license life light like limit
line link lint list listen little live load local lock log logger logging login logout long look loop low
machine main make manage manager manual many map mark markdown match math matrix max maybe measure media
memo memory menu merge message meta method middle middleware migrate migration min mirror miss mix mobile
mock mode model modern modify module money monitor month more mount mouse move much multi multiple must
mutate name native navigate need nest nested network new next node none normal note nothing notify null
number object observe offset ok old once one only open operation option optional order origin other out
outer output over override owner pack package padding page pager pagination paint panel param parameter
parent parse parser part partial pass password paste patch path pattern pause pay peer per perform
permission persist person pick picture pipe pipeline place plain plan platform play plot plugin point
pointer policy poll pool pop popup port portal position post power practice prefix prepare present press
pretty prevent preview primary print private probe problem process produce product profile program progress
project promise prompt proof prop property protect provide provider proxy public publish pull push put
query queue quick quit race raise random range rate raw reach react read ready real reason receive record
redirect reduce reducer ref refactor reference refresh region register regular reject relate relation
release reload remote remove render repeat replace reply report repository request require reset resize
resolve resource response rest result resume retry return reuse reverse review rich right role roll root
rotate round route router row rule run safe same sample save scale scan schedule schema scope score screen
script scroll search second secret section secure security see seed select selector self send sensor
separate sequence serial serialize serve server service session set setting setup shadow shape share sheet
shell shift short show side sign signal simple single site size skip slice slide slow small smart snapshot
socket soft solid solve some sort source space span spawn special speed split spread sql stack stage
standard star start state static status step stop storage store stream strict string strip strong struct
structure style submit subscribe success suggest sum super support swap switch symbol sync syntax system tab
table tag tail take target task team template temp term test text theme thing thread throttle throw tick
time timeout timer title today toggle token tool top topic total touch trace track transform transition
translate tree trigger trim true try tune turn tutorial type ui undo unique unit unknown update upgrade
upload url usage use user utility valid validate validation value var variable vector verify version video
view virtual visit visual void volume wait walk want warn warning watch way web week weight while white
widget width window word work worker world wrap write wrong yaml year yield zero zone
`)

// isCommonWord reports a name that is a common word, in any case, plural,
// with -ing or -ed, or with a .js suffix.
func isCommonWord(name string) bool {
	k := strings.ToLower(trimJSSuffix(name))
	if commonWords[k] || commonWords[stem(k)] {
		return true
	}
	for _, suffix := range []string{"ing", "ed"} {
		base, ok := strings.CutSuffix(k, suffix)
		if !ok || len(base) < 3 {
			continue
		}
		// configured → configure, routing → route, running → run, parsed → parse
		if commonWords[base] || commonWords[base+"e"] || base[len(base)-1] == base[len(base)-2] && commonWords[base[:len(base)-1]] {
			return true
		}
	}
	return false
}
