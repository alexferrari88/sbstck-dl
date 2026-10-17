package cmd

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

func newExportEPUBCommand() *cobra.Command {
	var options lib.EPUBOptions
	command := &cobra.Command{
		Use:   "export-epub",
		Short: "Export a local HTML archive as an offline EPUB book",
		Long: "Export all eligible posts in an existing HTML archive as one EPUB, oldest first. " +
			"Images must already be local; no network connection or credentials are used. " +
			"Missing required content fails without replacing an existing book. PDF export is deferred.",
		Args: cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			for _, flag := range []string{"before", "after"} {
				if cmd.Flags().Changed(flag) {
					return fmt.Errorf("--%s is not supported for EPUB export; all local HTML posts are included", flag)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer cancel()
			if err := lib.ExportEPUB(ctx, options); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "EPUB book generated: %s\n", options.Output)
			return nil
		},
	}
	command.Flags().StringVarP(&options.Input, "input", "i", ".", "Existing HTML archive directory")
	command.Flags().StringVarP(&options.Output, "output", "o", "", "Destination EPUB file")
	command.Flags().StringVar(&options.Title, "title", "", "Book title (required)")
	command.Flags().StringVar(&options.Author, "author", "", "Book author, if known")
	command.Flags().StringVar(&options.Language, "language", "und", "Book language tag (default: undetermined)")
	command.MarkFlagRequired("output")
	command.MarkFlagRequired("title")
	return command
}
