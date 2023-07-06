package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/alexferrari88/sbstck-dl/lib"
)

/*

sbstck-dl —  Substack Downloader

Usage:
  sbstck-dl [command]

Available Commands:
  download    Download individual posts or entire public archive
  list        List all available posts based on filters
  version     Display the current version of the app

Flags:
  -h, --help                Help for sbstck-dl
  -v, --verbose             Enable verbose output
  -p, --path string         Specify the download directory (default is "./downloads")
  -f, --format string       Specify the output format (options: "html", "md", "txt", default is "html")
  -r, --retry int           Specify the number of retries on network failure (default is 3)
  -l, --limit-rate int      Limit the number of requests per minute (default is 60)
  -c, --config string       Specify the path of a configuration file

Use "sbstck-dl [command] --help" for more information about a command.

Examples:

  # Download a single post
  sbstck-dl download --url https://substackurl//some-blog-post

  # Download an entire public archive
  sbstck-dl download --url https://substackurl/post/

  # List posts without downloading
  sbstck-dl list --url https://substackurl/post/

  # Download with a configuration file
  sbstck-dl download --config /path/to/config.yml

*/

func main() {

	// parse the --url flag
	// if url is not provided, print usage and exit
	// if url is provided, check if it's a valid substack url
	url := flag.String("url", "", "Specify the substack url")
	flag.Parse()

	if *url == "" {
		flag.Usage()
		return
	}

	e := lib.NewExtractor(nil)

	// if url contains /p/ then it's a single post
	// if url contains /archive/ then it's an archive
	if strings.Contains(*url, "/p/") {
		// download single post
		post, err := e.ExtractPost(context.TODO(), *url)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%+v\n", post)
	}

}
