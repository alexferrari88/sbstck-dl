package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	archives := make(map[string]*lib.Archive)
	if createArchive && !dryRun {
		for _, selected := range formats {
			archive, err := lib.LoadArchive(outputFolder, selected)
			if err != nil {
				return fmt.Errorf("restore archive %s (previous index preserved): %w", selected, err)
			}
			archives[selected] = archive
		}
	}
	var failures []error
	bodyImages := make(map[string]*lib.ImageDownloadResult)
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
			if !complete {
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
	recordImages := func(path string, images *lib.ImageDownloadResult) {
		if images == nil {
			return
		}
		key := filepath.Clean(path)
		if bodyImages[key] == nil {
			bodyImages[key] = &lib.ImageDownloadResult{}
		}
		bodyImages[key].Images = append(bodyImages[key].Images, images.Images...)
	}

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
		// Share proofs with retained and new entries, even if an output write fails.
		for _, selected := range formats {
			recordImages(makePath(post, outputFolder, selected), images)
			if archive := archives[selected]; archive != nil {
				for _, entry := range archive.Entries {
					if entry.Post.Slug == post.Slug {
						recordImages(entry.FilePath, images)
					}
				}
			}
		}
		if mediaErr != nil {
			failures = append(failures, fmt.Errorf("write post %s: %w", post.Slug, mediaErr))
			continue
		}
		postWritten := false
		for _, selected := range missing {
			path := makePath(post, outputFolder, selected)
			if verbose {
				fmt.Printf("Writing post to file %s\n", path)
			}
			output := prepared
			if selected == "txt" {
				output = post
			}
			if err := output.WriteToFile(path, selected, addSourceURL); err != nil {
				failures = append(failures, fmt.Errorf("write post %s: %w", path, err))
				if archive := archives[selected]; archive != nil {
					removeInvalidArchiveEntry(archive, path)
				}
			} else {
				postWritten = true
				if inventory[post.Slug] == nil {
					inventory[post.Slug] = make(map[string]bool)
				}
				inventory[post.Slug][selected] = true
				if archive := archives[selected]; archive != nil {
					archive.AddEntry(post, path, time.Now())
				}
			}
		}
		if postWritten {
			downloadedPosts++
		}
		if verbose && images != nil && images.Success > 0 {
			fmt.Printf("Downloaded %d images (%d failed) for post %s\n", images.Success, images.Failed, post.Slug)
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(append(failures, err)...)
	}
	if verbose {
		fmt.Printf("Downloaded %d posts, out of %d\nDone in %s\n", downloadedPosts, len(urls), time.Since(startTime))
	}

	if len(archives) == 0 {
		return errors.Join(failures...)
	}
	if _, err := os.Stat(outputFolder); os.IsNotExist(err) {
		return errors.Join(failures...)
	}
	if downloadImages {
		combined := lib.NewArchive()
		for _, selected := range formats {
			combined.Entries = append(combined.Entries, archives[selected].Entries...)
		}
		downloader := lib.NewImageDownloader(fetcher, outputFolder, imagesDir, lib.ImageQuality(imageQuality))
		warnings, err := combined.LocalizeCovers(ctx, downloader, bodyImages)
		for _, warning := range warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", warning)
		}
		if err != nil {
			return errors.Join(append(failures, fmt.Errorf("localize archive covers: %w", err))...)
		}
		offset := 0
		for _, selected := range formats {
			n := len(archives[selected].Entries)
			archives[selected].Entries = combined.Entries[offset : offset+n]
			offset += n
		}
	}
	for _, selected := range formats {
		archive := archives[selected]
		if err := archive.SaveMetadata(outputFolder, selected); err != nil {
			failures = append(failures, fmt.Errorf("save archive metadata %s (previous index preserved): %w", selected, err))
			continue
		}
		if verbose {
			fmt.Printf("Generating archive page in %s format...\n", selected)
		}
		var err error
		switch selected {
		case "html":
			err = archive.GenerateHTML(outputFolder)
		case "md":
			err = archive.GenerateMarkdown(outputFolder)
		case "txt":
			err = archive.GenerateText(outputFolder)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("publish archive %s (previous index preserved): %w", selected, err))
		} else if verbose {
			fmt.Printf("Archive page generated: %s/index.%s\n", outputFolder, selected)
		}
	}
	return errors.Join(failures...)
}
