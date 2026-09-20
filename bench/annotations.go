package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Validate annotations against source HTML, never candidate Markdown. This
// catches stale links, contradictory exclusions and checks with no source
// evidence. Code indentation and reference scope still require human review.
func validateReference(f fixture) error {
	f, err := materialize(f)
	if err != nil {
		return err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(f.HTML))
	if err != nil {
		return err
	}
	root := doc.Find(f.Spec.Selector)
	if root.Length() == 0 {
		return fmt.Errorf("%s: reference selector matches no source elements", f.Spec.ID)
	}
	if overlappingRoots(root) {
		return fmt.Errorf("%s: overlapping reference roots would count content twice", f.Spec.ID)
	}
	// Keep the original document for noise evidence, but positive assertions
	// must belong to the annotated content after its exclusions are applied.
	root = root.Clone()
	if f.Spec.Exclude != "" {
		root.Find(f.Spec.Exclude).Remove()
	}
	base, err := url.Parse(f.Spec.FinalURL)
	if err != nil {
		return err
	}
	var problems []error
	for _, a := range f.Spec.Checks {
		if !sourceHas(doc, root, base, f.Reference, a) {
			problems = append(problems, fmt.Errorf("%s/%s: annotation has no matching source evidence (or contradicts reference)", f.Spec.ID, a.ID))
		}
	}
	return errors.Join(problems...)
}

func overlappingRoots(root *goquery.Selection) bool {
	nodes := make(map[*html.Node]bool)
	for _, n := range root.Nodes {
		nodes[n] = true
	}
	for _, n := range root.Nodes {
		for parent := n.Parent; parent != nil; parent = parent.Parent {
			if nodes[parent] {
				return true
			}
		}
	}
	return false
}

func validateReferences(fixtures []fixture) error {
	var problems []error
	for _, f := range fixtures {
		problems = append(problems, validateReference(f))
	}
	return errors.Join(problems...)
}

func sourceHas(doc *goquery.Document, root *goquery.Selection, base *url.URL, reference string, a assertion) bool {
	switch a.Kind {
	case "text":
		return containsPhrase(reference, a.Text) && containsPhrase(plainText(root), a.Text)
	case "code":
		return containsPhrase(reference, a.Text) && selectionHas(root.Find("pre"), func(s *goquery.Selection) bool {
			return containsPhrase(plainText(s), a.Text)
		})
	case "heading":
		return containsPhrase(reference, a.Text) && selectionHas(root.Find("h1,h2,h3,h4,h5,h6").AddBackFiltered("h1,h2,h3,h4,h5,h6"), func(s *goquery.Selection) bool {
			return normalized(plainText(s)) == normalized(a.Text)
		})
	case "absent":
		return containsPhrase(plainText(doc.Selection), a.Text) && !containsPhrase(reference, a.Text)
	case "table":
		return selectionHas(root.Find("table"), func(s *goquery.Selection) bool { return tableMatches(s, a) })
	case "link":
		return selectionHas(root.Find("a[href],img[src]"), func(s *goquery.Selection) bool {
			for _, attr := range []string{"href", "src"} {
				value, ok := s.Attr(attr)
				u, err := url.Parse(value)
				if ok && err == nil && base.ResolveReference(u).String() == a.Text {
					return true
				}
			}
			return false
		})
	}
	return false
}
