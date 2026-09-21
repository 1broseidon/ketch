package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/scrape"
	"github.com/andybalholm/cascadia"
	"github.com/spf13/cobra"
)

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Convert piped HTML to clean markdown",
	Long: `Read raw HTML from stdin, run ketch's extraction pipeline, and write
clean markdown for LLM workflows. The extract_mode config key applies.

Examples:
  curl -L https://chain.sh/ketch | ketch extract
  curl -L https://example.com | ketch extract --url https://example.com
  cat page.html | ketch extract --select article --max-chars 4000
  xclip -selection clipboard -o | ketch extract --trim --json

For fetching URLs directly, use ketch scrape. extract never fetches,
caches, renders a browser, or probes /llms.txt.`,
	Args: exitArgs(cobra.NoArgs),
	RunE: runExtract,
}

func init() {
	rootCmd.AddCommand(extractCmd)
	extractCmd.Flags().String("url", "", "source URL for metadata and relative-link resolution")
	extractCmd.Flags().String("select", "", "CSS selector to extract specific elements (skips readability)")
	extractCmd.Flags().Int("max-chars", 0, "truncate markdown output to N chars (0 = disabled)")
	extractCmd.Flags().Bool("trim", false, "strip markdown formatting, keep content text only")
}

type extractOptions struct {
	URL      string
	Selector string
	Trim     bool
	MaxChars int
	JSON     bool
	Mode     extract.Mode // pruning mode from config extract_mode; empty = clean
}

type extractedPage struct {
	URL      string
	Title    string
	Markdown string
}

type extractJSON struct {
	URL      string `json:"url,omitempty"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
	Words    int    `json:"words"`
}

func runExtract(cmd *cobra.Command, _ []string) error {
	opts := extractOptions{
		URL:      stringFlag(cmd, "url"),
		Selector: stringFlag(cmd, "select"),
		Trim:     boolFlag(cmd, "trim"),
		MaxChars: intFlag(cmd, "max-chars"),
	}
	opts.JSON, _ = cmd.Root().PersistentFlags().GetBool("json")
	mode, err := extract.ParseMode(cfg.ExtractMode)
	if err != nil {
		return exitErrf(ExitValidation, "invalid extract_mode: %w", err)
	}
	opts.Mode = mode

	if !stdinIsPipe() {
		return exitErrf(ExitValidation, "pipe HTML to stdin (for URLs use ketch scrape <url>)")
	}

	rawHTML, err := readExtractInput(os.Stdin)
	if err != nil {
		return err
	}

	page, err := extractFromHTML(rawHTML, opts)
	if err != nil {
		return err
	}

	return emitExtract(os.Stdout, page, opts.JSON)
}

func stringFlag(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func boolFlag(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func intFlag(cmd *cobra.Command, name string) int {
	v, _ := cmd.Flags().GetInt(name)
	return v
}

func readExtractInput(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, scrape.MaxBodyBytes+1))
	if err != nil {
		return "", exitErrf(ExitValidation, "failed to read stdin: %w", err)
	}
	if bytes.HasPrefix(b, []byte("%PDF-")) {
		return "", exitErrf(ExitValidation, "PDF input detected. Use `ketch scrape <url>` for PDF extraction.")
	}
	if len(b) > scrape.MaxBodyBytes {
		return "", exitErrf(ExitValidation, "stdin exceeds %d byte limit", scrape.MaxBodyBytes)
	}
	return string(b), nil
}

func extractFromHTML(rawHTML string, opts extractOptions) (*extractedPage, error) {
	if strings.TrimSpace(rawHTML) == "" {
		return nil, exitErrf(ExitValidation, "stdin is empty (pipe HTML to stdin; for URLs use ketch scrape <url>)")
	}

	urlFlag := opts.URL
	if urlFlag != "" {
		_, err := url.Parse(urlFlag)
		if err != nil {
			return nil, exitErrf(ExitValidation, "invalid --url: %w", err)
		}
	}

	if opts.Selector != "" {
		if _, err := cascadia.Parse(opts.Selector); err != nil {
			return nil, exitErrf(ExitValidation, "selector extraction failed: %w", err)
		}
		markdown, err := extract.ExtractSelectorWithURL(urlFlag, rawHTML, opts.Selector)
		if err != nil {
			return nil, exitErrf(ExitValidation, "selector extraction failed: %w", err)
		}
		if markdown == "" {
			return nil, exitErrf(ExitNotFound, "no elements matched selector %q", opts.Selector)
		}
		page := &extractedPage{
			URL:      urlFlag,
			Title:    extract.Title(rawHTML),
			Markdown: markdown,
		}
		page.Markdown = extract.PostProcess(page.Markdown, opts.Trim, opts.MaxChars)
		return page, nil
	}

	pageURL := urlFlag
	if pageURL == "" {
		pageURL = "about:blank"
	}

	res, err := extract.NewWithMode(opts.Mode).Extract(pageURL, rawHTML)
	if err != nil {
		return nil, exitErrf(ExitUpstream, "extraction failed: %w", err)
	}

	title := res.Title
	if title == "" {
		title = extract.Title(rawHTML)
	}

	page := &extractedPage{
		URL:      urlFlag,
		Title:    title,
		Markdown: res.Markdown,
	}
	page.Markdown = extract.PostProcess(page.Markdown, opts.Trim, opts.MaxChars)
	return page, nil
}

func emitExtract(w io.Writer, page *extractedPage, asJSON bool) error {
	if asJSON {
		out := extractJSON{
			URL:      page.URL,
			Title:    page.Title,
			Markdown: page.Markdown,
			Words:    len(strings.Fields(page.Markdown)),
		}
		return json.NewEncoder(w).Encode(out)
	}

	words := len(strings.Fields(page.Markdown))
	if _, err := fmt.Fprintln(w, "---"); err != nil {
		return err
	}
	if page.URL != "" {
		if _, err := fmt.Fprintf(w, "url: %s\n", page.URL); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "title: %s\n", page.Title); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "words: %d\n", words); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "---"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, page.Markdown); err != nil {
		return err
	}
	return nil
}
