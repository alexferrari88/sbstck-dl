package cmd

import (
	"context"
	"log"
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
	ctx            = context.Background()
	parsedProxyURL *url.URL
	fetcher        *lib.Fetcher
	extractor      *lib.Extractor

	rootCmd = &cobra.Command{
		Use:   "sbstck-dl",
		Short: "Substack Downloader",
		Long:  `sbstck-dl is a command line tool for downloading Substack newsletters for archival purposes, offline reading, or data analysis.`,
		// Uncomment the following line if your bare application
		// has an action associated with it:
		// Run: func(cmd *cobra.Command, args []string) { },
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if proxyURL != "" {
		var err error
		parsedProxyURL, err = parseURL(proxyURL)
		if err != nil {
			log.Fatal(err)
		}
	}
	fetcher = lib.NewFetcher(ratePerSecond, parsedProxyURL)
	extractor = lib.NewExtractor(fetcher)
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.sbstck-dl.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.PersistentFlags().StringVarP(&proxyURL, "proxy", "x", "", "Specify the proxy url")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().IntVarP(&ratePerSecond, "rate", "r", 10, "Specify the rate of requests per second")
}
