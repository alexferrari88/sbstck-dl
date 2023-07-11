package cmd

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// downloadCmd represents the download command
var (
	downloadUrl  string
	format       string
	outputFolder string
	dryRun       bool
	downloadCmd  = &cobra.Command{
		Use:   "download",
		Short: "Download individual posts or the entire public archive",
		Long:  `You can provide the url of a single post or the main url of the Substack you want to download.`,
		Run: func(cmd *cobra.Command, args []string) {
			startTime := time.Now()

			// if url contains "/p/", we are downloading a single post
			if strings.Contains(downloadUrl, "/p/") {
				if verbose {
					fmt.Printf("Downloading post %s\n", downloadUrl)
				}
				if dryRun {
					fmt.Println("Dry run, exiting...")
					return
				}

				post, err := extractor.ExtractPost(ctx, downloadUrl)
				if err != nil {
					log.Fatal(err)
				}
				downloadTime := time.Since(startTime)
				if verbose {
					fmt.Printf("Downloaded post %s in %s\n", downloadUrl, downloadTime)
				}

				path := makePath(post, outputFolder, format)
				if verbose {
					fmt.Printf("Writing post to file %s\n", path)
				}

				post.WriteToFile(path, format)

				if verbose {
					fmt.Println("Done in ", time.Since(startTime))
				}
			} else {
				// we are downloading the entire archive
				urls, err := extractor.GetAllPostsURLs(ctx, downloadUrl)
				if err != nil {
					log.Fatal(err)
				}
				if verbose {
					fmt.Printf("Found %d posts\n", len(urls))
				}
				if dryRun {
					fmt.Printf("Found %d posts\n", len(urls))
					fmt.Println("Dry run, exiting...")
					return
				}
				bar := progressbar.NewOptions(len(urls),
					progressbar.OptionSetWidth(25),
					progressbar.OptionSetDescription("downloading"),
					progressbar.OptionShowBytes(true))
				for result := range extractor.ExtractAllPosts(ctx, urls) {
					if result.Err != nil {
						log.Fatal(result.Err)
					}
					bar.Add(1)
					if verbose {
						fmt.Printf("Downloading post %s\n", result.Post.CanonicalUrl)
					}
					post := result.Post

					path := makePath(post, outputFolder, format)
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
	downloadCmd.PersistentFlags().StringVarP(&downloadUrl, "url", "u", "", "Specify the Substack url")
	downloadCmd.PersistentFlags().StringVarP(&format, "format", "f", "html", "Specify the output format (options: \"html\", \"md\", \"txt\"")
	downloadCmd.PersistentFlags().StringVarP(&outputFolder, "path", "p", ".", "Specify the download directory")
	downloadCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "d", false, "Enable dry run")
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

func validateURL(toTest string) (*url.URL, error) {
	_, err := url.ParseRequestURI(toTest)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(toTest)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, err
	}

	return u, err
}

func makePath(post lib.Post, outputFolder string, format string) string {
	return fmt.Sprintf("%s/%s_%s.%s", outputFolder, convertDateTime(post.PostDate), post.Slug, format)
}
