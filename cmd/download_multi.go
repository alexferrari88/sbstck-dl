package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// pflag consumes one value per flag. Fold only the contiguous value run after
// a format flag; other arguments remain for Cobra's strict argument validation.
func normalizeFormatArgs(args []string) []string {
	out := make([]string, 0, len(args))
	lookup := func(name string, short bool) *pflag.Flag {
		if short {
			if flag := downloadCmd.Flags().ShorthandLookup(name); flag != nil {
				return flag
			}
			return rootCmd.PersistentFlags().ShorthandLookup(name)
		}
		if flag := downloadCmd.Flags().Lookup(name); flag != nil {
			return flag
		}
		return rootCmd.PersistentFlags().Lookup(name)
	}
	for i := 0; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			return append(out, args[i:]...)
		}
		isFormat := token == "--format" || token == "-f"
		inline := strings.HasPrefix(token, "--format=") || (strings.HasPrefix(token, "-f") && len(token) > 2)
		if !isFormat && !inline {
			out = append(out, token)
			// Preserve a non-format option's value verbatim, even if it starts -f.
			needsValue := false
			if strings.HasPrefix(token, "--") && !strings.Contains(token, "=") {
				if flag := lookup(strings.TrimPrefix(token, "--"), false); flag != nil {
					needsValue = flag.NoOptDefVal == ""
				}
			} else if strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "--") {
				for j := 1; j < len(token); j++ {
					flag := lookup(token[j:j+1], true)
					if flag == nil {
						break
					}
					if flag.NoOptDefVal == "" {
						needsValue = j == len(token)-1
						break
					}
				}
			}
			if needsValue && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}
		var values []string
		if inline {
			if strings.HasPrefix(token, "--format=") {
				values = append(values, strings.TrimPrefix(token, "--format="))
			} else {
				values = append(values, strings.TrimPrefix(strings.TrimPrefix(token, "-f"), "="))
			}
		}
		for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			values = append(values, args[i])
		}
		if len(values) == 0 {
			out = append(out, token)
			continue
		}
		out = append(out, "--format="+strings.Join(values, ","))
	}
	return out
}

func requestedFormats() ([]string, error) {
	values := formatValues
	if len(values) == 0 {
		// Keep direct RunE/helper callers compatible with the old single selection.
		if format != "" {
			values = []string{format}
		} else {
			values = []string{"html"}
		}
	}
	var result []string
	seen := make(map[string]bool)
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			switch item {
			case "html", "md", "txt":
			default:
				return nil, fmt.Errorf("unknown or empty output format: %q (use html, md, txt)", item)
			}
			if !seen[item] {
				seen[item] = true
				result = append(result, item)
			}
		}
	}
	return result, nil
}

// Inventory once: filename/version identities match archive restoration, and
// symlinks, directories and empty files never count as complete downloads.
var localPostName = regexp.MustCompile(`^(\d{8}_\d{6})?_(.+)\.(html|md|txt)$`)

func existingPostInventory(folder string) (map[string]map[string]bool, error) {
	inventory := make(map[string]map[string]bool)
	files, err := os.ReadDir(folder)
	if os.IsNotExist(err) {
		return inventory, nil
	}
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		match := localPostName.FindStringSubmatch(file.Name())
		if match == nil || !file.Type().IsRegular() {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return nil, err
		}
		if info.Size() == 0 {
			continue
		}
		if inventory[match[2]] == nil {
			inventory[match[2]] = make(map[string]bool)
		}
		inventory[match[2]][match[3]] = true
	}
	return inventory, nil
}

func runDownload(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return cobra.NoArgs(cmd, args)
	}
	startTime := time.Now()
	formats, err := requestedFormats()
	if err != nil {
		return err
	}
	var archiveWorkflow *lib.ArchiveWorkflow
	if createArchive && !dryRun {
		archiveWorkflow, err = lib.LoadArchiveWorkflow(outputFolder, formats)
		if err != nil {
			return err
		}
	}
	var failures []error
	single := strings.Contains(downloadUrl, "/p/")
	var urls []string
	if single {
		urls = []string{downloadUrl}
	} else {
		urls, err = extractor.GetAllPostsURLs(ctx, downloadUrl, makeDateFilterFunc(beforeDate, afterDate))
		if err != nil {
			return err
		}
	}
	if verbose {
		fmt.Printf("Found %d posts\n", len(urls))
	}
	if dryRun {
		fmt.Printf("Found %d posts\nDry run, exiting...\n", len(urls))
		return nil
	}
	// The media manifest records posts whose media is complete, so later runs
	// can skip them
	var manifest *lib.MediaManifest
	if downloadPodcasts || downloadVideos {
		manifest, err = lib.LoadMediaManifest(outputFolder)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: ignoring unreadable media manifest: %v\n", err)
			manifest = lib.NewMediaManifest(outputFolder)
		}
	}
	inventory := make(map[string]map[string]bool)
	if !single {
		inventory, err = existingPostInventory(outputFolder)
		if err != nil {
			return fmt.Errorf("inspect existing posts: %w", err)
		}
		var needed []string
		for _, url := range urls {
			complete := true
			for _, selected := range formats {
				if !inventory[extractSlug(url)][selected] {
					complete = false
				}
			}
			// Posts already downloaded are revisited for media that isn't
			// complete yet according to the manifest, so media can be added
			// to an existing download.
			if !complete || (manifest != nil && postNeedsMedia(manifest, extractSlug(url))) {
				needed = append(needed, url)
			}
		}
		urls = needed
	}
	var bar *progressbar.ProgressBar
	if !single && len(urls) > 0 {
		bar = progressbar.NewOptions(len(urls), progressbar.OptionSetWidth(25), progressbar.OptionSetDescription("downloading"), progressbar.OptionShowBytes(true))
	}
	downloadedPosts := 0
	var extensions []string
	if fileExtensions != "" {
		extensions = strings.Split(strings.ReplaceAll(fileExtensions, " ", ""), ",")
	}
	for result := range extractor.ExtractAllPosts(ctx, urls) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if result.Err != nil {
			if single {
				return result.Err
			}
			if verbose {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error downloading post: %v\n", result.Err)
			}
			continue
		}
		if bar != nil {
			bar.Add(1)
		}
		post := result.Post
		if verbose {
			fmt.Printf("Downloading post %s\n", post.CanonicalUrl)
		}
		var missing []string
		for _, selected := range formats {
			if single || !inventory[post.Slug][selected] {
				missing = append(missing, selected)
			}
		}
		// Media files share the base name of the post files
		mediaPath := makePath(post, outputFolder, formats[0])
		var media lib.EmbeddedMedia
		if manifest != nil {
			if verbose && len(missing) == 0 {
				fmt.Printf("Post already downloaded, only downloading media: %s\n", post.Slug)
			}
			media = downloadPostMedia(manifest, post, mediaPath)
			// HTML written by an earlier run gets the media embedded in place
			if inventory[post.Slug]["html"] && !slices.Contains(missing, "html") {
				htmlPath := makePath(post, outputFolder, "html")
				if err := embedMediaInFile(htmlPath, post, media); err != nil {
					failures = append(failures, fmt.Errorf("embed media in %s: %w", htmlPath, err))
				}
			}
		}
		if len(missing) == 0 {
			continue
		}
		filesSupported := false
		for _, selected := range missing {
			if selected != "txt" {
				filesSupported = true
			}
		}
		prepared, images, mediaErr := post.PrepareMedia(ctx, outputFolder, downloadImages, lib.ImageQuality(imageQuality), imagesDir, downloadFiles && filesSupported, extensions, filesDir, fetcher)
		if mediaErr != nil {
			if archiveWorkflow != nil {
				archiveWorkflow.RecordPost(post, images, nil)
			}
			failures = append(failures, fmt.Errorf("write post %s: %w", post.Slug, mediaErr))
			continue
		}
		prepared.BodyHTML = lib.EmbedMedia(prepared.BodyHTML, post.VideoUploadId, media)
		postWritten := false
		var writes []lib.ArchivePostWrite
		for _, selected := range missing {
			path := makePath(post, outputFolder, selected)
			if verbose {
				fmt.Printf("Writing post to file %s\n", path)
			}
			output := prepared
			if selected == "txt" {
				output = post
			}
			writeErr := output.WriteToFile(path, selected, addSourceURL)
			writes = append(writes, lib.ArchivePostWrite{Format: selected, Path: path, DownloadTime: time.Now(), Err: writeErr})
			if writeErr != nil {
				failures = append(failures, fmt.Errorf("write post %s: %w", path, writeErr))
			} else {
				postWritten = true
				if inventory[post.Slug] == nil {
					inventory[post.Slug] = make(map[string]bool)
				}
				inventory[post.Slug][selected] = true
			}
		}
		if archiveWorkflow != nil {
			archiveWorkflow.RecordPost(post, images, writes)
		}
		if postWritten {
			downloadedPosts++
		}
		if images != nil {
			if verbose {
				fmt.Printf("Downloaded %d images (%d failed) for post %s\n", images.Success, images.Failed, post.Slug)
			}
			for _, image := range images.Images {
				if image.Error != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: image %s: %v; using remote source\n", image.OriginalURL, image.Error)
				}
			}
		}

	}
	if err := ctx.Err(); err != nil {
		return errors.Join(append(failures, err)...)
	}
	if verbose {
		fmt.Printf("Downloaded %d posts, out of %d\nDone in %s\n", downloadedPosts, len(urls), time.Since(startTime))
	}

	if archiveWorkflow == nil {
		return errors.Join(failures...)
	}
	var downloader *lib.ImageDownloader
	if downloadImages {
		downloader = lib.NewImageDownloader(fetcher, outputFolder, imagesDir, lib.ImageQuality(imageQuality))
	}
	publication := archiveWorkflow.Publish(ctx, downloader)
	for _, warning := range publication.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", warning)
	}
	if verbose {
		for _, selected := range publication.PublishedFormats {
			fmt.Printf("Archive page generated: %s/index.%s\n", outputFolder, selected)
		}
	}
	return errors.Join(append(failures, publication.Err)...)
}
