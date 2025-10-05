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

	notesCmd = &cobra.Command{
		Use:   "notes",
		Short: "Download Substack Notes for a specific user",
		Long: `Download all Substack Notes for a specific user using their user ID.

Notes are stored as comments in the user's activity feed. This command fetches
activity pages, filters out regular comments when requested, and writes notes in
HTML, Markdown, or plain text formats with timestamped filenames.

Example usage:
  sbstck-dl notes --user-id 303863305 --username nweiss --output-dir ./notes
  sbstck-dl notes --user-id 303863305 --format md --max-pages 5`,
		Run: func(cmd *cobra.Command, args []string) {
			if notesUserID == "" {
				log.Fatal("user-id is required")
			}

			// normalise format
			notesFormat = strings.ToLower(notesFormat)
			switch notesFormat {
			case "html", "md", "txt":
			default:
				log.Fatalf("unsupported format: %s", notesFormat)
			}

			// prepare output path
			outputDir := notesOutputDir
			if notesUsername != "" {
				outputDir = filepath.Join(notesOutputDir, notesUsername)
			} else {
				outputDir = filepath.Join(notesOutputDir, fmt.Sprintf("user_%s", notesUserID))
			}

			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				log.Fatalf("error creating output directory: %v", err)
			}

			fmt.Printf("Downloading notes for user ID: %s\n", notesUserID)
			fmt.Printf("Output directory: %s\n", outputDir)
			fmt.Printf("Format: %s\n\n", notesFormat)

			client := lib.NewNotesClient(fetcher)

			items, err := client.FetchAllUserActivity(ctx, notesUserID, notesMaxPages, verbose)
			if err != nil {
				log.Fatalf("error fetching user activity: %v", err)
			}

			if len(items) == 0 {
				fmt.Println("No activity found for user")
				return
			}

			if verbose {
				fmt.Printf("Fetched %d activity items\n", len(items))
			}

			var notes []*lib.Note
			for _, item := range items {
				if item.Type != "comment" || item.Comment.ID == 0 {
					continue
				}

				if notesOnly && client.IsLikelyRegularComment(item.Comment, item) {
					continue
				}

				if note := client.ConvertCommentToNote(item.Comment, item); note != nil {
					notes = append(notes, note)
				}
			}

			if len(notes) == 0 {
				fmt.Println("No notes found in user activity")
				return
			}

			if verbose {
				fmt.Printf("Saving %d notes...\n", len(notes))
			}

			saved := 0
			for i, note := range notes {
				if verbose {
					fmt.Printf("[%d/%d] %s\n", i+1, len(notes), note.ID)
				}
				if err := client.SaveNote(note, outputDir, notesFormat); err != nil {
					log.Printf("error saving note %s: %v", note.ID, err)
					continue
				}
				saved++
			}

			fmt.Printf("Successfully saved %d note(s) to %s\n", saved, outputDir)
		},
	}
)

func init() {
	notesCmd.Flags().StringVar(&notesUserID, "user-id", "", "User ID (required, e.g., 303863305 for @nweiss)")
	notesCmd.Flags().StringVar(&notesUsername, "username", "", "Username for organising output (e.g., nweiss)")
	notesCmd.Flags().StringVar(&notesOutputDir, "output-dir", "./notes", "Output directory")
	notesCmd.Flags().StringVar(&notesFormat, "format", "md", "Output format (html, md, txt)")
	notesCmd.Flags().IntVar(&notesMaxPages, "max-pages", 10, "Maximum pages of activity to fetch")
	notesCmd.Flags().BoolVar(&notesOnly, "notes-only", false, "Attempt to filter regular comments and keep only notes")

	_ = notesCmd.MarkFlagRequired("user-id")
}
