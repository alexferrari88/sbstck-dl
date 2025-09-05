package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

var (
	notesUserID    string
	notesUsername  string
	notesOutputDir string
	notesFormat    string
	notesMaxPages  int
	notesOnly      bool
	notesCmd       = &cobra.Command{
		Use:   "notes",
		Short: "Download Substack Notes for a specific user",
		Long: `Download all Substack Notes for a specific user using their user ID.

Notes are stored as comments in the user's activity feed. This command fetches
all activity and filters for notes vs regular comments.

Example usage:
  sbstck-dl notes --user-id 303863305 --username nweiss --output-dir ./notes
  sbstck-dl notes --user-id 303863305 --format md --max-pages 5`,
		Run: func(cmd *cobra.Command, args []string) {
			if notesUserID == "" {
				log.Fatal("user-id is required")
			}

			// Setup output directory
			outputDir := notesOutputDir
			if notesUsername != "" {
				outputDir = filepath.Join(notesOutputDir, notesUsername)
			} else {
				outputDir = filepath.Join(notesOutputDir, fmt.Sprintf("user_%s", notesUserID))
			}

			// Create output directory
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				log.Fatalf("Error creating output directory: %v", err)
			}

			fmt.Printf("Downloading notes for user ID: %s\n", notesUserID)
			fmt.Printf("Output directory: %s\n", outputDir)
			fmt.Printf("Format: %s\n", notesFormat)
			fmt.Println()

			// Create notes client
			notesClient := lib.NewNotesClient(fetcher)

			// Fetch all notes/comments
			items, err := notesClient.FetchAllUserActivity(notesUserID, notesMaxPages, verbose)
			if err != nil {
				log.Fatalf("Error fetching user activity: %v", err)
			}

			if len(items) == 0 {
				fmt.Println("No activity found for user")
				return
			}

			fmt.Printf("Found %d total activity items\n", len(items))

			// Filter and process
			var notes []*lib.Note
			for _, item := range items {
				if item.Type == "comment" && item.Comment.ID != 0 {
					// Skip if trying to filter for notes only and this looks like a regular comment
					if notesOnly {
						if notesClient.IsLikelyRegularComment(item.Comment, item) {
							continue
						}
					}

					note := notesClient.ConvertCommentToNote(item.Comment, item)
					if note != nil {
						notes = append(notes, note)
					}
				}
			}

			fmt.Printf("Processing %d potential notes...\n", len(notes))
			fmt.Println()

			// Save all notes
			for i, note := range notes {
				if verbose {
					fmt.Printf("[%d/%d] Saving note: %s\n", i+1, len(notes), note.ID)
				}
				if err := notesClient.SaveNote(note, outputDir, notesFormat); err != nil {
					log.Printf("Error saving note %s: %v", note.ID, err)
				}
			}

			fmt.Printf("Successfully saved %d items to: %s\n", len(notes), outputDir)
		},
	}
)

func init() {
	notesCmd.Flags().StringVar(&notesUserID, "user-id", "", "User ID (required, e.g., 303863305 for @nweiss)")
	notesCmd.Flags().StringVar(&notesUsername, "username", "", "Username for organizing output (e.g., nweiss)")
	notesCmd.Flags().StringVar(&notesOutputDir, "output-dir", "./notes", "Output directory")
	notesCmd.Flags().StringVar(&notesFormat, "format", "md", "Output format (html, md, txt)")
	notesCmd.Flags().IntVar(&notesMaxPages, "max-pages", 10, "Maximum pages to fetch")
	notesCmd.Flags().BoolVar(&notesOnly, "notes-only", false, "Try to filter for notes vs regular comments")

	notesCmd.MarkFlagRequired("user-id")
}