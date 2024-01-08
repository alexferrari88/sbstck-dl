package cmd

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands

var (
	proxyURL       string
	verbose        bool
	ratePerSecond  int
	beforeDate     string
	afterDate      string
	substackID     string
	ctx            = context.Background()
	parsedProxyURL *url.URL
	fetcher        *lib.Fetcher
	extractor      *lib.Extractor

	rootCmd = &cobra.Command{
		Use:   "sbstck-dl",
		Short: "Substack Downloader",
		Long:  `sbstck-dl is a command line tool for downloading Substack newsletters for archival purposes, offline reading, or data analysis.`,
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	var cookie *http.Cookie
	if proxyURL != "" {
		var err error
		parsedProxyURL, err = parseURL(proxyURL)
		if err != nil {
			log.Fatal(err)
		}
	}
	if ratePerSecond == 0 {
		log.Fatal("rate must be greater than 0")
	}
	if substackID != "" {
		cookie = &http.Cookie{
			Name:  "substack.sid",
			Value: substackID,
		}
	}
	fetcher = lib.NewFetcher(lib.WithRatePerSecond(ratePerSecond), lib.WithProxyURL(parsedProxyURL), lib.WithCookie(cookie))
	extractor = lib.NewExtractor(fetcher)
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&proxyURL, "proxy", "x", "", "Specify the proxy url")
	rootCmd.PersistentFlags().StringVarP(&substackID, "sid", "i", "", "The substack.sid cookie value (required for private newsletters)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().IntVarP(&ratePerSecond, "rate", "r", lib.DefaultRatePerSecond, "Specify the rate of requests per second")
	rootCmd.PersistentFlags().StringVar(&beforeDate, "before", "", "Download posts published before this date (format: YYYY-MM-DD)")
	rootCmd.PersistentFlags().StringVar(&afterDate, "after", "", "Download posts published after this date (format: YYYY-MM-DD)")
}

func makeDateFilterFunc(beforeDate string, afterDate string) lib.DateFilterFunc {
	var dateFilterFunc lib.DateFilterFunc
	if beforeDate != "" && afterDate != "" {
		dateFilterFunc = func(date string) bool {
			return date > afterDate && date < beforeDate
		}
	} else if beforeDate != "" {
		dateFilterFunc = func(date string) bool {
			return date < beforeDate
		}
	} else if afterDate != "" {
		dateFilterFunc = func(date string) bool {
			return date > afterDate
		}
	}
	return dateFilterFunc
}
