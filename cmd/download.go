package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

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
			ctx := context.Background()
			fetcher := lib.NewFetcher(10, nil)
			extractor := lib.NewExtractor(fetcher)

			// if url contains "/p/", we are downloading a single post
			if strings.Contains(url, "/p/") {
				post, err := extractor.ExtractPost(ctx, url)
				if err != nil {
					panic(err)
				}
				// write post to file in the outputFolder
				// the file name should be the post slug
				// the file format should be the one specified in the format flag
				path := fmt.Sprintf("%s/%s_%s.%s", outputFolder, post.PostDate, post.Slug, format)
				f, err := os.Create(path)
				if err != nil {
					panic(err)
				}
				defer f.Close()
				var content string
				switch format {
				case "html":
					content = post.ToHTML(true)
				case "md":
					content, err = post.ToMD(true)
					if err != nil {
						panic(err)
					}
				case "txt":
					content = post.ToText(true)
				default:
					panic("Invalid format")
				}
				_, err = f.WriteString(content)
				if err != nil {
					panic(err)
				}
				f.Sync()
			} else {
				// we are downloading the entire archive
				for result := range extractor.ExtractAllPosts(ctx, url) {
					if result.Err != nil {
						panic(result.Err)
					}
					post := result.Post
					// write post to file in the outputFolder
					// the file name should be the post slug
					// the file format should be the one specified in the format flag
					path := fmt.Sprintf("%s/%s_%s.%s", outputFolder, post.PostDate, post.Slug, format)
					f, err := os.Create(path)
					if err != nil {
						panic(err)
					}
					defer f.Close()
					var content string
					switch format {
					case "html":
						content = post.ToHTML(true)
					case "md":
						content, err = post.ToMD(true)
						if err != nil {
							panic(err)
						}
					case "txt":
						content = post.ToText(true)
					default:
						panic("Invalid format")
					}
					_, err = f.WriteString(content)
					if err != nil {
						panic(err)
					}
					f.Sync()
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
