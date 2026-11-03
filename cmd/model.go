package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/internal/embed"
	"github.com/1broseidon/ketch/scrape"
	"github.com/spf13/cobra"
)

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Manage the embedding model the local docs index ranks with",
	Long: `Manage the embedding model the local docs index (ketch docs index, ketch docs -b local) ranks with.
The model is optional: without one, indexed docs are ranked by keywords. It runs inside ketch, on the CPU, with no other software.`,
}

var modelInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Download the embedding model (bge-base-en-v1.5; --small for bge-small-en-v1.5)",
	Long:  `Download the embedding model into ketch's cache directory: bge-base-en-v1.5 (438 MB), or with --small bge-small-en-v1.5 (133 MB), from Hugging Face at a pinned revision. Every file is checked against its pinned size and SHA-256. Re-running finishes an interrupted install and re-verifies the files already there.`,
	Args:  exitArgs(cobra.NoArgs),
	RunE:  runModelInstall,
}

var modelStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which embedding model is installed and which one ketch uses",
	Args:  exitArgs(cobra.NoArgs),
	RunE:  runModelStatus,
}

func init() {
	rootCmd.AddCommand(modelCmd)
	modelCmd.AddCommand(modelInstallCmd)
	modelCmd.AddCommand(modelStatusCmd)
	modelInstallCmd.Flags().Bool("small", false, "install bge-small-en-v1.5 (133 MB, 384 dimensions) instead of bge-base-en-v1.5")
}

func runModelInstall(cmd *cobra.Command, _ []string) error {
	spec := embed.Base
	if small, _ := cmd.Flags().GetBool("small"); small {
		spec = embed.Small
	}
	dir, err := embed.DefaultDir(spec)
	if err != nil {
		return exitErrf(ExitPrecondition, "model install: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Downloading %s (%.0f MB) from huggingface.co/%s at %s...\n", spec.Name, float64(spec.Size())/1e6, spec.ID, spec.Revision[:12])
	// No overall timeout: the model is hundreds of megabytes. The command's
	// context still cancels it.
	client := httpx.New(0, 2)
	if err := embed.Install(cmd.Context(), client, scrape.DefaultUserAgent(), spec, dir, os.Stderr); err != nil {
		return upstreamErr(err, "model install failed")
	}
	fmt.Fprintf(os.Stderr, "Installed to: %s\n", dir)
	if path := cfg.String("model_path"); path != "" {
		fmt.Fprintf(os.Stderr, "Note: model_path is set (%s) and takes precedence; unset it to use this model: ketch config set model_path \"\"\n", path)
	} else if active, err := embed.Locate(""); err == nil && active.Dir != dir {
		fmt.Fprintf(os.Stderr, "Note: %s is also installed and is used first; this model is used only without it.\n", active.ID)
	}
	fmt.Fprintln(os.Stderr, "Indexes built before with another model are re-embedded by: ketch docs index --reembed <library>")
	return nil
}

// modelStatus is `ketch model status --json`.
type modelStatus struct {
	ModelPath string            `json:"model_path,omitempty"`
	Active    *modelStatusEntry `json:"active"`
	Problem   string            `json:"problem,omitempty"`
	Installed []modelStatusSpec `json:"models"`
}

type modelStatusEntry struct {
	ID  string `json:"id"`
	Dir string `json:"dir"`
}

type modelStatusSpec struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Dir       string `json:"dir"`
	Installed bool   `json:"installed"`
	Problem   string `json:"problem,omitempty"`
}

func runModelStatus(cmd *cobra.Command, _ []string) error {
	st := modelStatus{ModelPath: cfg.String("model_path")}
	if m, err := embed.Locate(st.ModelPath); err == nil {
		st.Active = &modelStatusEntry{ID: m.ID, Dir: m.Dir}
	} else if !errors.Is(err, embed.ErrNotInstalled) {
		st.Problem = err.Error()
	}
	for _, s := range embed.Specs() {
		dir, err := embed.DefaultDir(s)
		if err != nil {
			return exitErrf(ExitPrecondition, "model status: %v", err)
		}
		c := embed.Check(s, dir)
		st.Installed = append(st.Installed, modelStatusSpec{Name: s.Name, ID: s.ID, Dir: dir, Installed: c.Installed, Problem: c.Problem})
	}
	if asJSON, _ := cmd.Root().PersistentFlags().GetBool("json"); asJSON {
		return json.NewEncoder(os.Stdout).Encode(st)
	}
	if st.ModelPath != "" {
		fmt.Printf("model_path: %s\n", st.ModelPath)
	}
	switch {
	case st.Active != nil:
		fmt.Printf("active: %s (%s)\n", st.Active.ID, st.Active.Dir)
	case st.Problem != "":
		fmt.Printf("active: none (%s)\n", st.Problem)
	default:
		fmt.Println("active: none (indexed docs are ranked by keywords; run: ketch model install)")
	}
	for _, s := range st.Installed {
		state := "installed"
		if !s.Installed {
			state = s.Problem
		}
		fmt.Printf("%s: %s (%s)\n", s.Name, state, s.Dir)
	}
	return nil
}
