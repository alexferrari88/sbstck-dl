package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

// listCmd represents the list command
var (
	pubUrl  string
	listCmd = &cobra.Command{
		Use:   "list",
		Short: "List the posts of a Substack",
		Long:  `List the posts of a Substack`,
		Run: func(cmd *cobra.Command, args []string) {
			parsedURL, err := parseURL(pubUrl)
			if err != nil {
				log.Fatal(err)
			}
			mainWebsite := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
			if verbose {
				fmt.Printf("Main website: %s\n", mainWebsite)
				fmt.Println("Getting all posts URLs...")
			}
			dateFilterfunc := makeDateFilterFunc(beforeDate, afterDate)
			urls, err := extractor.GetAllPostsURLs(ctx, mainWebsite, dateFilterfunc)
			if err != nil {
				log.Fatal(err)
			}
			if verbose {
				fmt.Printf("Found %d posts.\n", len(urls))
			}
			for _, url := range urls {
				fmt.Println(url)
			}
		},
	}
)

func init() {
	rootCmd.AddCommand(listCmd)

	listCmd.PersistentFlags().StringVarP(&pubUrl, "url", "u", "", "Specify the Substack url")
	listCmd.MarkPersistentFlagRequired("url")
}
