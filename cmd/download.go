package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

// downloadCmd represents the download command
var (
	url          string
	format       string
	outputFolder string
	verbose      bool
	downloadCmd  = &cobra.Command{
		Use:   "download",
		Short: "Download individual posts or the entire public archive",
		Long:  `You can provide the url of a single post or the main url of the Substack you want to download.`,
		Run: func(cmd *cobra.Command, args []string) {
			startTime := time.Now()
			ctx := context.Background()
			fetcher := lib.NewFetcher(10, nil)
			extractor := lib.NewExtractor(fetcher)

			// if url contains "/p/", we are downloading a single post
			if strings.Contains(url, "/p/") {
				if verbose {
					fmt.Printf("Downloading post %s\n", url)
				}
				post, err := extractor.ExtractPost(ctx, url)
				if err != nil {
					panic(err)
				}
				downloadTime := time.Since(startTime)
				if verbose {
					fmt.Printf("Downloaded post %s in %s\n", url, downloadTime)
				}
				// write post to file in the outputFolder
				// the file name should be the post slug
				// the file format should be the one specified in the format flag
				path := fmt.Sprintf("%s/%s_%s.%s", outputFolder, convertDateTime(post.PostDate), post.Slug, format)
				if verbose {
					fmt.Printf("Writing post to file %s\n", path)
				}
				post.WriteToFile(path, format)
				if verbose {
					fmt.Println("Done in ", time.Since(startTime))
				}
			} else {
				// we are downloading the entire archive
				for result := range extractor.ExtractAllPosts(ctx, url) {
					if result.Err != nil {
						panic(result.Err)
					}
					if verbose {
						fmt.Printf("Downloading post %s\n", result.Post.CanonicalUrl)
					}
					post := result.Post
					// write post to file in the outputFolder
					// the file name should be the post slug
					// the file format should be the one specified in the format flag
					path := fmt.Sprintf("%s/%s_%s.%s", outputFolder, post.PostDate, post.Slug, format)
					if verbose {
						fmt.Printf("Writing post to file %s\n", path)
					}
					post.WriteToFile(path, format)
				}
				if verbose {
					fmt.Println("Done in ", time.Since(startTime))
				}
			}
		},
	}
)

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.PersistentFlags().StringVarP(&url, "url", "u", "", "Specify the Substack url")
	downloadCmd.PersistentFlags().StringVarP(&format, "format", "f", "html", "Specify the output format (options: \"html\", \"md\", \"txt\"")
	downloadCmd.PersistentFlags().StringVarP(&outputFolder, "path", "p", ".", "Specify the download directory")
	downloadCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	downloadCmd.MarkPersistentFlagRequired("url")

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// downloadCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// downloadCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func convertDateTime(datetime string) string {
	// Parse the datetime string
	parsedTime, err := time.Parse(time.RFC3339, datetime)
	if err != nil {
		// Return an empty string or an error message if parsing fails
		return ""
	}

	// Format the datetime to the desired format
	formattedDateTime := fmt.Sprintf("%d%02d%02d_%02d%02d%02d",
		parsedTime.Year(), parsedTime.Month(), parsedTime.Day(),
		parsedTime.Hour(), parsedTime.Minute(), parsedTime.Second())

	return formattedDateTime
}
