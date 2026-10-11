package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// downloadCmd represents the download command
var (
	downloadUrl    string
	format         string
	outputFolder   string
	dryRun         bool
	addSourceURL   bool
	downloadImages bool
	imageQuality   string
	imagesDir      string
	downloadFiles  bool
	fileExtensions string
	filesDir       string
	createArchive  bool
	downloadCmd    = &cobra.Command{
		Use:   "download",
		Short: "Download individual posts or the entire public archive",
		Long:  `You can provide the url of a single post or the main url of the Substack you want to download.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			startTime := time.Now()

			// Create archive instance if flag is set
			var archive *lib.Archive
			if createArchive && !dryRun {
				var err error
				archive, err = lib.LoadArchive(outputFolder, format)
				if err != nil {
					return fmt.Errorf("restore archive (previous index preserved): %w", err)
				}
			}
			var writeErrors []error
			bodyImages := make(map[string]*lib.ImageDownloadResult)
			recordImages := func(path string, result *lib.ImageDownloadResult) {
				if archive == nil || result == nil {
					return
				}
				key := filepath.Clean(path)
				if bodyImages[key] == nil {
					bodyImages[key] = &lib.ImageDownloadResult{}
				}
				bodyImages[key].Images = append(bodyImages[key].Images, result.Images...)
			}

			// if url contains "/p/", we are downloading a single post
			if strings.Contains(downloadUrl, "/p/") {
				if verbose {
					fmt.Printf("Downloading post %s\n", downloadUrl)
				}
				if dryRun {
					fmt.Println("Dry run, exiting...")
					return nil
				}
				if (beforeDate != "" || afterDate != "") && verbose {
					fmt.Println("Warning: --before and --after flags are ignored when downloading a single post")
				}

				post, err := extractor.ExtractPost(ctx, downloadUrl)
				if err != nil {
					return err
				}
				downloadTime := time.Since(startTime)
				if verbose {
					fmt.Printf("Downloaded post %s in %s\n", downloadUrl, downloadTime)
				}

				path := makePath(post, outputFolder, format)
				if verbose {
					fmt.Printf("Writing post to file %s\n", path)
				}

				imageResult, err := writeDownloadedPost(post, path)
				recordImages(path, imageResult)
				if err != nil {
					writeErrors = append(writeErrors, fmt.Errorf("write post %s: %w", path, err))
					if archive != nil {
						removeInvalidArchiveEntry(archive, path)
					}
				} else if archive != nil {
					archive.AddEntry(post, path, startTime)
				}

				if verbose {
					fmt.Println("Done in ", time.Since(startTime))
				}
			} else {
				// we are downloading the entire archive
				var downloadedPostsCount int
				dateFilterfunc := makeDateFilterFunc(beforeDate, afterDate)
				urls, err := extractor.GetAllPostsURLs(ctx, downloadUrl, dateFilterfunc)
				urlsCount := len(urls)
				if err != nil {
					return err
				}
				if urlsCount == 0 && archive == nil {
					if verbose {
						fmt.Println("No posts found, exiting...")
					}
					return nil
				}
				if verbose {
					fmt.Printf("Found %d posts\n", urlsCount)
				}
				if dryRun {
					fmt.Printf("Found %d posts\n", urlsCount)
					fmt.Println("Dry run, exiting...")
					return nil
				}
				urls, err = filterExistingPosts(urls, outputFolder, format)
				if err != nil {
					if verbose {
						fmt.Println("Error filtering existing posts:", err)
					}
				}
				if len(urls) == 0 && archive == nil {
					if verbose {
						fmt.Println("No new posts found, exiting...")
					}
					return nil
				}
				bar := progressbar.NewOptions(len(urls),
					progressbar.OptionSetWidth(25),
					progressbar.OptionSetDescription("downloading"),
					progressbar.OptionShowBytes(true))
				for result := range extractor.ExtractAllPosts(ctx, urls) {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
					if result.Err != nil {
						if verbose {
							fmt.Printf("Error downloading post %s: %s\n", result.Post.CanonicalUrl, result.Err)
							fmt.Println("Skipping...")
						}
						continue
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

					imageResult, err := writeDownloadedPost(post, path)
					recordImages(path, imageResult)
					if err != nil {
						writeErrors = append(writeErrors, fmt.Errorf("write post %s: %w", path, err))
						if archive != nil {
							removeInvalidArchiveEntry(archive, path)
						}
					} else {
						downloadedPostsCount++
						if archive != nil {
							archive.AddEntry(post, path, time.Now())
						}
					}
				}
				if verbose {
					fmt.Println("Downloaded", downloadedPostsCount, "posts, out of", len(urls))
					fmt.Println("Done in ", time.Since(startTime))
				}
			}

			// Generate archive page if enabled
			if archive != nil {
				if _, err := os.Stat(outputFolder); os.IsNotExist(err) {
					return errors.Join(writeErrors...)
				}
				if downloadImages {
					downloader := lib.NewImageDownloader(fetcher, outputFolder, imagesDir, lib.ImageQuality(imageQuality))
					warnings, err := archive.LocalizeCovers(ctx, downloader, bodyImages)
					for _, warning := range warnings {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", warning)
					}
					if err != nil {
						return errors.Join(append(writeErrors, fmt.Errorf("localize archive covers: %w", err))...)
					}
				}
				if err := archive.SaveMetadata(outputFolder, format); err != nil {
					return errors.Join(append(writeErrors, fmt.Errorf("save archive metadata (previous index preserved): %w", err))...)
				}
				if verbose {
					fmt.Printf("Generating archive page in %s format...\n", format)
				}

				var archiveErr error
				switch format {
				case "html":
					archiveErr = archive.GenerateHTML(outputFolder)
				case "md":
					archiveErr = archive.GenerateMarkdown(outputFolder)
				case "txt":
					archiveErr = archive.GenerateText(outputFolder)
				default:
					archiveErr = fmt.Errorf("unknown format for archive: %s", format)
				}

				if archiveErr != nil {
					writeErrors = append(writeErrors, fmt.Errorf("publish archive (previous index preserved): %w", archiveErr))
				} else if verbose {
					fmt.Printf("Archive page generated: %s/index.%s\n", outputFolder, format)
				}
			}
			return errors.Join(writeErrors...)
		},
	}
)

func init() {
	downloadCmd.Flags().StringVarP(&downloadUrl, "url", "u", "", "Specify the Substack url")
	downloadCmd.Flags().StringVarP(&format, "format", "f", "html", "Specify the output format (options: \"html\", \"md\", \"txt\"")
	downloadCmd.Flags().StringVarP(&outputFolder, "output", "o", ".", "Specify the download directory")
	downloadCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Enable dry run")
	downloadCmd.Flags().BoolVar(&addSourceURL, "add-source-url", false, "Add the original post URL at the end of the downloaded file")
	downloadCmd.Flags().BoolVar(&downloadImages, "download-images", false, "Download images locally and update content to reference local files")
	downloadCmd.Flags().StringVar(&imageQuality, "image-quality", "high", "Image quality to download (options: \"high\", \"medium\", \"low\")")
	downloadCmd.Flags().StringVar(&imagesDir, "images-dir", "images", "Directory name for downloaded images")
	downloadCmd.Flags().BoolVar(&downloadFiles, "download-files", false, "Download file attachments locally and update content to reference local files")
	downloadCmd.Flags().StringVar(&fileExtensions, "file-extensions", "", "Comma-separated list of file extensions to download (e.g., 'pdf,docx,txt'). If empty, downloads all file types")
	downloadCmd.Flags().StringVar(&filesDir, "files-dir", "files", "Directory name for downloaded file attachments")
	downloadCmd.Flags().BoolVar(&createArchive, "create-archive", false, "Create an archive index page linking all downloaded posts")
	downloadCmd.MarkFlagRequired("url")
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

func parseURL(toTest string) (*url.URL, error) {
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

// extractSlug extracts the slug from a Substack post URL
// e.g. https://example.substack.com/p/this-is-the-post-title -> this-is-the-post-title
func extractSlug(url string) string {
	split := strings.Split(url, "/")
	return split[len(split)-1]
}

// filterExistingPosts filters out posts that already exist in the output folder.
// It looks for files whose name ends with the post slug.
func filterExistingPosts(urls []string, outputFolder string, format string) ([]string, error) {
	var filtered []string
	for _, url := range urls {
		slug := extractSlug(url)
		path := fmt.Sprintf("%s/%s_%s.%s", outputFolder, "*", slug, format)
		matches, err := filepath.Glob(path)
		if err != nil {
			return urls, err
		}
		if len(matches) == 0 {
			filtered = append(filtered, url)
		}
	}
	return filtered, nil
}

// The same success check applies to plain posts and posts with downloaded media.
func writeDownloadedPost(post lib.Post, path string) (*lib.ImageDownloadResult, error) {
	if !downloadImages && !downloadFiles {
		return nil, post.WriteToFile(path, format, addSourceURL)
	}
	var extensions []string
	if fileExtensions != "" {
		extensions = strings.Split(strings.ReplaceAll(fileExtensions, " ", ""), ",")
	}
	result, err := post.WriteToFileWithImages(ctx, path, format, addSourceURL, downloadImages, lib.ImageQuality(imageQuality), imagesDir, downloadFiles, extensions, filesDir, fetcher)
	if err == nil && verbose && result.Success > 0 {
		fmt.Printf("Downloaded %d images (%d failed) for post %s\n", result.Success, result.Failed, post.Slug)
	}
	return result, err
}

// Atomic post writes preserve a valid prior file when replacement fails.
func removeInvalidArchiveEntry(archive *lib.Archive, path string) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		archive.RemoveEntry(path)
	}
}
