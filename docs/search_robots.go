package docs

// Robots and the pages a search returned (design/docs-evidence.md, Final
// shape, Third pass). `ketch scrape` and `ketch crawl` do not consult
// robots.txt. A page `ketch docs` reads at its exact address because the
// search returned it (at most sfExactPages per call) is read the same way,
// as the caller reading that result would read it: a site that closes an
// old release to search engines (python.org's /3.10/) is still read when
// the search ranked that page. Pages ketch finds by itself (widening, a
// site's lists, discovery, `ketch docs index`) and the page a library
// value or resolve names keep honouring robots.txt. The question that
// names no library reads only search results (search_bare.go) and so
// never consults it.

// robotsApply reports a candidate of a site's exact pages that robots.txt
// is consulted for: one the search did not return (rank 0: the page a
// library URL names, the module page or docs root resolve found).
func robotsApply(c sfCand) bool { return c.rank == 0 }
