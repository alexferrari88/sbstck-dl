package cmd

import (
	"fmt"
	"log"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

var (
	updateArchiveOutputFolder string
	updateArchiveFormat       string
	updateArchiveCmd          = &cobra.Command{
		Use:   "update-archive-page",
		Short: "Regenerate the archive index page from existing posts on disk",
		Long: `Scans the output directory for existing downloaded posts and regenerates
the archive index page (index.html, index.md, or index.txt) with all posts found.

This is useful when:
- You've manually added/removed post files
- You want to regenerate the archive after changing formats
- The archive page was deleted or corrupted`,
		Run: func(cmd *cobra.Command, args []string) {
			if verbose {
				fmt.Printf("Scanning for existing posts in %s (format: %s)\n", updateArchiveOutputFolder, updateArchiveFormat)
			}

			// Scan existing posts
			archive, err := lib.ScanExistingPosts(updateArchiveOutputFolder, updateArchiveFormat)
			if err != nil {
				log.Fatalf("Error scanning posts: %v\n", err)
			}

			if len(archive.Entries) == 0 {
				fmt.Println("No posts found in the output directory.")
				return
			}

			if verbose {
				fmt.Printf("Found %d posts\n", len(archive.Entries))
			}

			// Generate archive page
			var archiveErr error
			switch updateArchiveFormat {
			case "html":
				archiveErr = archive.GenerateHTML(updateArchiveOutputFolder)
			case "md":
				archiveErr = archive.GenerateMarkdown(updateArchiveOutputFolder)
			case "txt":
				archiveErr = archive.GenerateText(updateArchiveOutputFolder)
			default:
				log.Fatalf("Unknown format: %s\n", updateArchiveFormat)
			}

			if archiveErr != nil {
				log.Fatalf("Error generating archive page: %v\n", archiveErr)
			}

			fmt.Printf("Archive page generated: %s/index.%s (%d posts)\n", updateArchiveOutputFolder, updateArchiveFormat, len(archive.Entries))
		},
	}
)

func init() {
	updateArchiveCmd.Flags().StringVarP(&updateArchiveOutputFolder, "output", "o", ".", "Directory containing downloaded posts")
	updateArchiveCmd.Flags().StringVarP(&updateArchiveFormat, "format", "f", "html", "Format of existing posts and archive page (options: \"html\", \"md\", \"txt\")")
}
