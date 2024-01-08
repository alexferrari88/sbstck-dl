# Substack Downloader

Simple CLI tool to download one or all the posts from a Substack blog.

## Installation

```bash
go install github.com/alexferrari88/sbstck-dl
```

## Usage

```bash
Usage:
  sbstck-dl [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  download    Download individual posts or the entire public archive
  help        Help about any command
  list        List the posts of a Substack
  version     Print the version number of sbstck-dl

Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
  -h, --help            help for sbstck-dl
  -x, --proxy string    Specify the proxy url
  -i, --sid string      Specify the substack.sid required to download private newsletters (found in the Substack's cookie)
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output

Use "sbstck-dl [command] --help" for more information about a command.
```

### Downloading posts

You can provide the url of a single post or the main url of the Substack you want to download.

```bash
Usage:
  sbstck-dl download [flags]

Flags:
  -d, --dry-run         Enable dry run
  -f, --format string   Specify the output format (options: "html", "md", "txt" (default "html")
  -h, --help            help for download
  -o, --output string   Specify the download directory (default ".")
  -u, --url string      Specify the Substack url

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

### Listing posts

```bash
Usage:
  sbstck-dl list [flags]

Flags:
  -h, --help         help for list
  -u, --url string   Specify the Substack url

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

## Thanks

- [wemoveon2](https://github.com/wemoveon2) for the discussion and help implementing the support for private newsletters

## TODO

- [x] Add support for private newsletters
- [ ] Improve retry logic
- [x] Implement filtering
- [ ] Implement loading from config file
- [ ] Add support for downloading media
- [ ] Add tests
- [ ] Add CI
- [x] Add documentation
