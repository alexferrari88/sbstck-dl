package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

var (
	archiveOutputFolder string
	archiveFormat       string

	updateArchiveCmd = &cobra.Command{
		Use:   "update-archive-page",
		Short: "Rebuild an archive index from local posts without downloading",
		Long:  "Rebuild one HTML, Markdown, or text archive index using local posts and retained archive metadata. No Substack URL or network connection is required.",
		Args:  cobra.NoArgs,
		// Offline publication does not need the root's download fetcher or its
		// proxy and rate validation.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {},
		RunE:             runUpdateArchive,
	}
)

func init() {
	updateArchiveCmd.Flags().StringVarP(&archiveOutputFolder, "output", "o", ".", "Specify the existing archive directory")
	updateArchiveCmd.Flags().StringVarP(&archiveFormat, "format", "f", "html", "Archive format: html, md, txt")
}

func runUpdateArchive(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	switch archiveFormat {
	case "html", "md", "txt":
	default:
		return fmt.Errorf("unknown archive format: %q (use html, md, txt)", archiveFormat)
	}
	info, err := os.Stat(archiveOutputFolder)
	if err != nil {
		return fmt.Errorf("inspect archive directory %q: %w", archiveOutputFolder, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("archive output %q is not a directory", archiveOutputFolder)
	}
	workflow, err := lib.LoadArchiveWorkflow(archiveOutputFolder, []string{archiveFormat})
	if err != nil {
		return err
	}
	publication := workflow.Publish(cmd.Context(), nil)
	if publication.Err != nil {
		return publication.Err
	}
	if len(publication.PublishedFormats) != 1 {
		return fmt.Errorf("archive index was not published in %q (directory may have been removed)", archiveOutputFolder)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Archive page generated: %s\n", filepath.Join(archiveOutputFolder, "index."+archiveFormat))
	return nil
}
