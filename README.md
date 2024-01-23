# Substack Downloader

Simple CLI tool to download one or all the posts from a Substack blog.

## Installation

### Downloading the binary

Check in the [releases](https://github.com/alexferrari88/sbstck-dl/releases) page for the latest version of the binary for your platform.
We provide binaries for Linux, MacOS and Windows.

### Using Go

```bash
go install github.com/alexferrari88/sbstck-dl
```

## Usage

```bash
Usage:
  sbstck-dl [command]

Available Commands:
  download    Download individual posts or the entire public archive
  help        Help about any command
  list        List the posts of a Substack
  version     Print the version number of sbstck-dl

Flags:
      --after string             Download posts published after this date (format: YYYY-MM-DD)
      --before string            Download posts published before this date (format: YYYY-MM-DD)
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters)
  -h, --help                     help for sbstck-dl
  -x, --proxy string             Specify the proxy url
  -r, --rate int                 Specify the rate of requests per second (default 2)
  -v, --verbose                  Enable verbose output

Use "sbstck-dl [command] --help" for more information about a command.
```

### Downloading posts

You can provide the url of a single post or the main url of the Substack you want to download.

By providing the main URL of a Substack, the downloader will download all the posts of the archive.

When downloading the full archive, if the downloader is interrupted, at the next execution it will resume the download of the remaining posts.

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
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters)
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
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

### Private Newsletters

In order to download the full text of private newsletters you need to provide the cookie name and value of your session.
The cookie name is either `substack.sid` or `connect.sid`, based on your cookie.
To get the cookie value you can use the developer tools of your browser.
Once you have the cookie name and value, you can pass them to the downloader using the `--cookie_name` and `--cookie_val` flags.

#### Example

```bash
sbstck-dl download --url https://example.substack.com --cookie_name substack.sid --cookie_val COOKIE_VALUE
```

## Thanks

- [wemoveon2](https://github.com/wemoveon2) and [lenzj](https://github.com/lenzj) for the discussion and help implementing the support for private newsletters

## TODO

- [ ] Improve retry logic
- [ ] Implement loading from config file
- [ ] Add support for downloading media
- [ ] Add tests
- [ ] Add CI
- [x] Add documentation
- [x] Add support for private newsletters
- [x] Implement filtering by date
- [x] Implement resuming downloads
