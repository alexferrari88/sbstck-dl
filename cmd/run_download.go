package cmd

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/schollz/progressbar/v3"
)

func runDownload(ctx context.Context, observer DownloadObserver, useProgressBar bool) (downloadSummary, error) {
	startTime := time.Now()
	mode := "archive"
	if strings.Contains(downloadUrl, "/p/") {
		mode = "single"
	}
	summary := downloadSummary{Mode: mode}
	skipExistingMode := !forceDownload
	if skipExisting {
		skipExistingMode = true
	}
	layout = normalizeLayout(layout)
	if !isValidLayout(layout) {
		return summary, fmt.Errorf("invalid --layout %q (use flat, year/month, or year/slug)", layout)
	}
	outputFolder = resolveOutputFolder(outputFolder, downloadUrl)
	failedURLs := make([]string, 0)
	failedURLSet := make(map[string]struct{})
	failFastMode := failFast
	if continueOnErr {
		failFastMode = false
	}

	if observer != nil && fetcher != nil {
		fetcher.OnRetry = func(info lib.RetryInfo) {
			emitDownloadEvent(observer, DownloadEvent{
				Type:       DownloadEventRetry,
				URL:        info.URL,
				RetryCount: info.Count,
				RetryWait:  info.Wait,
			})
		}
	}

	recordFailedURL := func(url string) {
		if url == "" {
			return
		}
		if _, exists := failedURLSet[url]; exists {
			return
		}
		failedURLSet[url] = struct{}{}
		failedURLs = append(failedURLs, url)
	}

	finalize := func() {
		logSummary(summary)
		if len(failedURLs) == 0 {
			return
		}
		path, err := writeFailedURLs(outputFolder, failedURLs)
		if err != nil {
			if logFormat == logFormatJSON {
				logEvent("failed_urls.write_failed", map[string]any{
					"path":  path,
					"error": err.Error(),
				})
			} else {
				log.Printf("Error writing failed URLs file: %v\n", err)
			}
			return
		}
		if logFormat == logFormatJSON {
			logEvent("failed_urls.written", map[string]any{
				"path":  path,
				"count": len(failedURLs),
			})
		} else {
			fmt.Printf("Failed URLs saved to %s\n", path)
		}
	}

	abort := func(message string) error {
		if logFormat == logFormatJSON {
			logEvent("download.aborted", map[string]any{
				"reason": message,
			})
		}
		return fmt.Errorf(message)
	}

	emitDownloadEvent(observer, DownloadEvent{Type: DownloadEventStart, URL: downloadUrl})

	logEvent("download.start", map[string]any{
		"mode":    mode,
		"url":     downloadUrl,
		"format":  format,
		"output":  outputFolder,
		"dry_run": dryRun,
		"layout":  layout,
	})

	manifestPath := filepath.Join(outputFolder, lib.ManifestFilename)
	manifest, err := lib.LoadManifest(manifestPath)
	if err != nil {
		if logFormat == logFormatJSON {
			logEvent("manifest.load_failed", map[string]any{
				"path":  manifestPath,
				"error": err.Error(),
			})
		} else {
			log.Printf("Error loading manifest: %v\n", err)
		}
		manifest = lib.NewManifest()
	}

	updateManifest := func(post lib.Post, path string, downloadedAt time.Time, lastModified string) {
		if manifest == nil {
			return
		}
		canonicalURL := post.CanonicalUrl
		if canonicalURL == "" {
			if verbose {
				if logFormat == logFormatJSON {
					logEvent("manifest.skipped", map[string]any{
						"slug":   post.Slug,
						"reason": "missing canonical URL",
					})
				} else {
					log.Printf("Skipping manifest entry for %s: missing canonical URL\n", post.Slug)
				}
			}
			return
		}
		if err := manifest.UpdateEntry(canonicalURL, path, outputFolder, format, downloadedAt, lastModified); err != nil {
			if logFormat == logFormatJSON {
				logEvent("manifest.update_failed", map[string]any{
					"url":   canonicalURL,
					"error": err.Error(),
				})
			} else {
				log.Printf("Error updating manifest for %s: %v\n", canonicalURL, err)
			}
			return
		}
		if err := manifest.Save(manifestPath); err != nil {
			if logFormat == logFormatJSON {
				logEvent("manifest.save_failed", map[string]any{
					"path":  manifestPath,
					"error": err.Error(),
				})
			} else {
				log.Printf("Error saving manifest: %v\n", err)
			}
		}
	}

	// Create archive instance if flag is set
	var archive *lib.Archive
	if createArchive {
		archive = lib.NewArchive()
	}

	// if url contains "/p/", we are downloading a single post
	if strings.Contains(downloadUrl, "/p/") {
		emitDownloadEvent(observer, DownloadEvent{Type: DownloadEventPlan, Total: 1})
		if verbose {
			fmt.Printf("Downloading post %s\n", downloadUrl)
		}
		if dryRun {
			summary.Skipped = 1
			logEvent("download.skipped", map[string]any{
				"url":    downloadUrl,
				"reason": "dry-run",
			})
			emitDownloadEvent(observer, DownloadEvent{
				Type:    DownloadEventPostSkipped,
				URL:     downloadUrl,
				Reason:  "dry-run",
				Skipped: 1,
			})
			finalize()
			fmt.Println("Dry run, exiting...")
			emitDownloadEvent(observer, DownloadEvent{
				Type:       DownloadEventSummary,
				Downloaded: summary.Downloaded,
				Skipped:    summary.Skipped,
				Failed:     summary.Failed,
			})
			return summary, nil
		}
		if (beforeDate != "" || afterDate != "") && verbose {
			fmt.Println("Warning: --before and --after flags are ignored when downloading a single post")
		}

		post, err := extractor.ExtractPost(ctx, downloadUrl)
		if err != nil {
			summary.Failed = 1
			recordFailedURL(downloadUrl)
			logEvent("download.failed", map[string]any{
				"url":   downloadUrl,
				"error": err.Error(),
			})
			emitDownloadEvent(observer, DownloadEvent{
				Type:  DownloadEventPostFailed,
				URL:   downloadUrl,
				Error: err.Error(),
			})
			finalize()
			return summary, err
		}
		downloadTime := time.Since(startTime)
		if verbose {
			fmt.Printf("Downloaded post %s in %s\n", downloadUrl, downloadTime)
		}

		path := makePath(post, outputFolder, format, layout)
		emitDownloadEvent(observer, DownloadEvent{
			Type:  DownloadEventPostStart,
			URL:   post.CanonicalUrl,
			Title: post.Title,
			Path:  path,
		})
		if verbose {
			fmt.Printf("Writing post to file %s\n", path)
		}

		var writeErr error
		if downloadImages || downloadFiles {
			imageQualityEnum := lib.ImageQuality(imageQuality)
			// Parse file extensions if specified
			var fileExtensionsSlice []string
			if fileExtensions != "" {
				fileExtensionsSlice = strings.Split(strings.ReplaceAll(fileExtensions, " ", ""), ",")
			}
			imageResult, err := post.WriteToFileWithImages(ctx, path, format, addSourceURL, downloadImages, imageQualityEnum, imagesDir, downloadFiles, fileExtensionsSlice, filesDir, fetcher)
			writeErr = err
			if writeErr != nil {
				if logFormat != logFormatJSON {
					log.Printf("Error writing file %s: %v\n", path, writeErr)
				}
			} else if verbose && imageResult.Success > 0 {
				fmt.Printf("Downloaded %d images (%d failed) for post %s\n", imageResult.Success, imageResult.Failed, post.Slug)
			}
		} else {
			writeErr = post.WriteToFile(path, format, addSourceURL)
			if writeErr != nil {
				if logFormat != logFormatJSON {
					log.Printf("Error writing file %s: %v\n", path, writeErr)
				}
			}
		}

		if writeErr == nil {
			downloadedAt := time.Now()
			summary.Downloaded = 1
			logEvent("download.completed", map[string]any{
				"url":  post.CanonicalUrl,
				"path": path,
			})
			updateManifest(post, path, downloadedAt, "")
			if writeMetadata {
				writeMetadataSidecar(post, path, outputFolder, format, downloadedAt, "")
			}
			emitDownloadEvent(observer, DownloadEvent{
				Type:       DownloadEventPostDone,
				URL:        post.CanonicalUrl,
				Path:       path,
				Title:      post.Title,
				Downloaded: 1,
			})
		} else {
			summary.Failed = 1
			if post.CanonicalUrl != "" {
				recordFailedURL(post.CanonicalUrl)
			} else {
				recordFailedURL(downloadUrl)
			}
			logEvent("download.failed", map[string]any{
				"url":   post.CanonicalUrl,
				"path":  path,
				"error": writeErr.Error(),
			})
			emitDownloadEvent(observer, DownloadEvent{
				Type:  DownloadEventPostFailed,
				URL:   post.CanonicalUrl,
				Error: writeErr.Error(),
			})
			if failFastMode {
				finalize()
				return summary, abort("Stopping due to --fail-fast")
			}
		}

		// Add to archive if enabled
		if archive != nil {
			archive.AddEntry(post, path, startTime)
		}

		if verbose {
			fmt.Println("Done in ", time.Since(startTime))
		}
		finalize()
		emitDownloadEvent(observer, DownloadEvent{
			Type:       DownloadEventSummary,
			Downloaded: summary.Downloaded,
			Skipped:    summary.Skipped,
			Failed:     summary.Failed,
		})
	} else {
		// we are downloading the entire archive
		dateFilterfunc := makeDateFilterFunc(beforeDate, afterDate)
		entries, err := extractor.GetAllPostsEntries(ctx, downloadUrl, dateFilterfunc)
		urlsCount := len(entries)
		if err != nil {
			summary.Failed = 1
			logEvent("download.failed", map[string]any{
				"url":   downloadUrl,
				"error": err.Error(),
			})
			finalize()
			return summary, err
		}
		if urlsCount == 0 {
			if verbose {
				fmt.Println("No posts found, exiting...")
			}
			finalize()
			emitDownloadEvent(observer, DownloadEvent{
				Type:       DownloadEventSummary,
				Downloaded: summary.Downloaded,
				Skipped:    summary.Skipped,
				Failed:     summary.Failed,
			})
			return summary, nil
		}
		if verbose {
			fmt.Printf("Found %d posts\n", urlsCount)
		}
		if dryRun {
			summary.Skipped = urlsCount
			logEvent("download.skipped", map[string]any{
				"reason": "dry-run",
				"count":  urlsCount,
			})
			emitDownloadEvent(observer, DownloadEvent{
				Type:    DownloadEventPlan,
				Total:   urlsCount,
				Skipped: urlsCount,
			})
			finalize()
			fmt.Printf("Found %d posts\n", urlsCount)
			fmt.Println("Dry run, exiting...")
			emitDownloadEvent(observer, DownloadEvent{
				Type:       DownloadEventSummary,
				Downloaded: summary.Downloaded,
				Skipped:    summary.Skipped,
				Failed:     summary.Failed,
			})
			return summary, nil
		}
		refreshed := 0
		if skipExistingMode {
			var skippedExisting int
			entries, skippedExisting, refreshed, err = filterEntriesForDownload(entries, outputFolder, format, manifest, refreshUpdated)
			if err != nil {
				if verbose {
					fmt.Println("Error filtering existing posts:", err)
				}
			}
			if skippedExisting > 0 {
				summary.Skipped += skippedExisting
				logEvent("download.skipped_existing", map[string]any{
					"count": skippedExisting,
				})
			}
			if refreshed > 0 {
				logEvent("download.refresh", map[string]any{
					"count": refreshed,
				})
			}
		}
		emitDownloadEvent(observer, DownloadEvent{
			Type:      DownloadEventPlan,
			Total:     len(entries),
			Skipped:   summary.Skipped,
			Refreshed: refreshed,
		})
		if len(entries) == 0 {
			if verbose {
				fmt.Println("No new posts found, exiting...")
			}
			finalize()
			emitDownloadEvent(observer, DownloadEvent{
				Type:       DownloadEventSummary,
				Downloaded: summary.Downloaded,
				Skipped:    summary.Skipped,
				Failed:     summary.Failed,
			})
			return summary, nil
		}
		urls := entriesToURLs(entries)
		lastModByURL := entriesToLastMod(entries)

		var bar *progressbar.ProgressBar
		if useProgressBar {
			bar = progressbar.NewOptions(len(urls),
				progressbar.OptionSetWidth(25),
				progressbar.OptionSetDescription("downloading"),
				progressbar.OptionShowBytes(true))
		}

		for result := range extractor.ExtractAllPosts(ctx, urls) {
			if ctx.Err() != nil {
				finalize()
				return summary, ctx.Err()
			}
			if result.Err != nil {
				summary.Failed++
				recordFailedURL(result.Url)
				logEvent("download.failed", map[string]any{
					"url":   result.Url,
					"error": result.Err.Error(),
				})
				emitDownloadEvent(observer, DownloadEvent{
					Type:  DownloadEventPostFailed,
					URL:   result.Url,
					Error: result.Err.Error(),
				})
				if verbose {
					fmt.Printf("Error downloading post %s: %s\n", result.Post.CanonicalUrl, result.Err)
					fmt.Println("Skipping...")
				}
				if failFastMode {
					finalize()
					return summary, abort("Stopping due to --fail-fast")
				}
				continue
			}
			if bar != nil {
				bar.Add(1)
			}
			if verbose {
				fmt.Printf("Downloading post %s\n", result.Post.CanonicalUrl)
			}
			post := result.Post

			path := makePath(post, outputFolder, format, layout)
			emitDownloadEvent(observer, DownloadEvent{
				Type:  DownloadEventPostStart,
				URL:   post.CanonicalUrl,
				Title: post.Title,
				Path:  path,
			})
			if verbose {
				fmt.Printf("Writing post to file %s\n", path)
			}

			var writeErr error
			if downloadImages || downloadFiles {
				imageQualityEnum := lib.ImageQuality(imageQuality)
				// Parse file extensions if specified
				var fileExtensionsSlice []string
				if fileExtensions != "" {
					fileExtensionsSlice = strings.Split(strings.ReplaceAll(fileExtensions, " ", ""), ",")
				}
				imageResult, err := post.WriteToFileWithImages(ctx, path, format, addSourceURL, downloadImages, imageQualityEnum, imagesDir, downloadFiles, fileExtensionsSlice, filesDir, fetcher)
				writeErr = err
				if writeErr != nil {
					if logFormat != logFormatJSON {
						log.Printf("Error writing file %s: %v\n", path, writeErr)
					}
				} else if verbose && imageResult.Success > 0 {
					fmt.Printf("Downloaded %d images (%d failed) for post %s\n", imageResult.Success, imageResult.Failed, post.Slug)
				}
			} else {
				writeErr = post.WriteToFile(path, format, addSourceURL)
				if writeErr != nil {
					if logFormat != logFormatJSON {
						log.Printf("Error writing file %s: %v\n", path, writeErr)
					}
				}
			}

			if writeErr == nil {
				downloadedAt := time.Now()
				summary.Downloaded++
				logEvent("download.completed", map[string]any{
					"url":  post.CanonicalUrl,
					"path": path,
				})
				lastModified := lastModByURL[result.Url]
				updateManifest(post, path, downloadedAt, lastModified)
				if writeMetadata {
					writeMetadataSidecar(post, path, outputFolder, format, downloadedAt, lastModified)
				}
				emitDownloadEvent(observer, DownloadEvent{
					Type:       DownloadEventPostDone,
					URL:        post.CanonicalUrl,
					Path:       path,
					Title:      post.Title,
					Downloaded: summary.Downloaded,
				})
			} else {
				summary.Failed++
				if post.CanonicalUrl != "" {
					recordFailedURL(post.CanonicalUrl)
				} else {
					recordFailedURL(result.Url)
				}
				logEvent("download.failed", map[string]any{
					"url":   post.CanonicalUrl,
					"path":  path,
					"error": writeErr.Error(),
				})
				emitDownloadEvent(observer, DownloadEvent{
					Type:  DownloadEventPostFailed,
					URL:   post.CanonicalUrl,
					Error: writeErr.Error(),
				})
				if failFastMode {
					finalize()
					return summary, abort("Stopping due to --fail-fast")
				}
			}

			// Add to archive if enabled and post was successfully written
			if archive != nil {
				archive.AddEntry(post, path, time.Now())
			}
		}
		if verbose {
			fmt.Println("Downloaded", summary.Downloaded, "posts, out of", len(urls))
			fmt.Println("Done in ", time.Since(startTime))
		}
		finalize()
		emitDownloadEvent(observer, DownloadEvent{
			Type:       DownloadEventSummary,
			Downloaded: summary.Downloaded,
			Skipped:    summary.Skipped,
			Failed:     summary.Failed,
		})
	}

	// Generate archive page if enabled
	if archive != nil && len(archive.Entries) > 0 {
		if verbose {
			fmt.Printf("Generating archive page in %s format...\n", format)
		}

		var archiveErr error
		switch format {
		case "html":
			archiveErr = archive.GenerateHTML(outputFolder)
		case "md":
			archiveErr = archive.GenerateMarkdown(outputFolder)
		case "obsidian-md":
			archiveErr = archive.GenerateObsidianMarkdown(outputFolder)
		case "txt":
			archiveErr = archive.GenerateText(outputFolder)
		default:
			archiveErr = fmt.Errorf("unknown format for archive: %s", format)
		}

		if archiveErr != nil {
			if logFormat == logFormatJSON {
				logEvent("archive.generate_failed", map[string]any{
					"format": format,
					"error":  archiveErr.Error(),
				})
			} else {
				log.Printf("Error generating archive page: %v\n", archiveErr)
			}
		} else if verbose {
			fmt.Printf("Archive page generated: %s/index.%s\n", outputFolder, outputFormatExtension(format))
		}
	}

	if createArchive {
		if err := generateNotionIndex(outputFolder, format); err != nil {
			if logFormat == logFormatJSON {
				logEvent("notion_links.generate_failed", map[string]any{
					"error": err.Error(),
				})
			} else if verbose {
				log.Printf("Error generating Notion links index: %v\n", err)
			}
		} else if verbose {
			fmt.Printf("Notion links index generated: %s/%s, %s/%s\n", outputFolder, notionLinksHTML, outputFolder, notionLinksMD)
		}
	}

	return summary, nil
}
