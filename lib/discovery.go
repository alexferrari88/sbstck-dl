package lib

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const archivePageSize = 12
const maxArchivePages = 10000

// GetAllPostsURLs discovers the publication's publicly enumerable archive.
// A feed or the initial archive HTML page cannot establish completeness.
func (e *Extractor) GetAllPostsURLs(ctx context.Context, pubURL string, f DateFilterFunc) ([]string, error) {
	root, err := url.Parse(pubURL)
	if err != nil {
		return nil, err
	}
	if (root.Scheme != "http" && root.Scheme != "https") || root.Host == "" || root.User != nil {
		return nil, fmt.Errorf("invalid publication URL: %q", pubURL)
	}
	root.Path, root.RawPath, root.RawQuery, root.Fragment = "", "", "", ""
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	urls, sitemapErr := e.sitemapURLs(ctx, root, f)
	if sitemapErr == nil {
		return urls, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	urls, err = e.archiveURLs(ctx, root, f, maxArchivePages)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("sitemap discovery: %w", sitemapErr), err)
	}
	return urls, nil
}

func (e *Extractor) sitemapURLs(ctx context.Context, root *url.URL, f DateFilterFunc) ([]string, error) {
	endpoint := root.ResolveReference(&url.URL{Path: "/sitemap.xml"}).String()
	body, err := e.fetcher.FetchURL(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var sitemap struct {
		XMLName xml.Name
		URLs    []struct {
			Loc     string `xml:"loc"`
			Lastmod string `xml:"lastmod"`
		} `xml:"url"`
	}
	decoder := xml.NewDecoder(body)
	err = decoder.Decode(&sitemap)
	if err == nil && sitemap.XMLName.Local != "urlset" {
		err = fmt.Errorf("expected sitemap urlset, received %s", sitemap.XMLName.Local)
	}
	if err == nil {
		// XML permits trailing whitespace/comments, but not another document.
		for {
			token, nextErr := decoder.Token()
			if nextErr == io.EOF {
				break
			}
			if nextErr != nil {
				err = nextErr
				break
			}
			switch v := token.(type) {
			case xml.CharData:
				if strings.TrimSpace(string(v)) != "" {
					err = errors.New("unexpected data after sitemap")
				}
			case xml.Comment, xml.ProcInst:
			default:
				err = errors.New("unexpected XML after sitemap")
			}
			if err != nil {
				break
			}
		}
	}
	body.Close()
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(sitemap.URLs))
	for _, entry := range sitemap.URLs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.TrimSpace(entry.Loc) == "" {
			return nil, errors.New("sitemap entry has no location")
		}
		if !strings.Contains(entry.Loc, "/p/") {
			continue
		}
		if f != nil && !f(entry.Lastmod) {
			continue
		}
		_, err := publicationPostURL(root, entry.Loc)
		if err != nil {
			return nil, err
		}
		u, _ := url.Parse(entry.Loc)
		urls = append(urls, root.ResolveReference(u).String())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return urls, nil
}

type archivePost struct {
	ID           int64  `json:"id"`
	CanonicalURL string `json:"canonical_url"`
	PostDate     string `json:"post_date"`
}

func (e *Extractor) archiveURLs(ctx context.Context, root *url.URL, f DateFilterFunc, maxPages int) ([]string, error) {
	urls := make([]string, 0, 100)
	seenIDs := make(map[int64]bool)
	seenURLs := make(map[string]bool)
	offset := 0
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		endpoint := root.ResolveReference(&url.URL{Path: "/api/v1/archive"})
		endpoint.RawQuery = url.Values{"sort": {"new"}, "offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(archivePageSize)}}.Encode()
		posts, err := e.archivePage(ctx, endpoint.String())
		if err != nil {
			return nil, fmt.Errorf("archive discovery at %s: %w", endpoint, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(posts) == 0 {
			return urls, nil
		}
		newEntries := 0
		for _, post := range posts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if post.ID <= 0 {
				return nil, fmt.Errorf("archive offset %d contains invalid post ID", offset)
			}
			postURL, err := publicationPostURL(root, post.CanonicalURL)
			if err != nil {
				return nil, fmt.Errorf("archive post %d: %w", post.ID, err)
			}
			if f != nil {
				if _, err := time.Parse(time.RFC3339Nano, post.PostDate); err != nil {
					return nil, fmt.Errorf("archive post %d has invalid post_date: %w", post.ID, err)
				}
			}
			duplicate := seenIDs[post.ID] || seenURLs[postURL]
			if !duplicate {
				newEntries++
			}
			seenIDs[post.ID], seenURLs[postURL] = true, true
			if !duplicate && (f == nil || f(post.PostDate)) {
				urls = append(urls, postURL)
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if newEntries == 0 {
			return nil, fmt.Errorf("archive pagination made no progress at offset %d", offset)
		}
		offset += len(posts)
	}
	return nil, fmt.Errorf("archive discovery exceeded %d pages without reaching an empty page", maxPages)
}

func (e *Extractor) archivePage(ctx context.Context, endpoint string) ([]archivePost, error) {
	body, err := e.fetcher.FetchURL(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var posts []archivePost
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&posts); err != nil {
		return nil, err
	}
	if posts == nil {
		return nil, errors.New("expected archive JSON array, received null")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("unexpected data after archive JSON array")
	}
	return posts, nil
}

// Keep discovered URLs on the requested publication origin. Canonical URLs
// may name its custom-domain alias; extraction can follow the normal redirect
// without sending discovery requests or cookies to an arbitrary canonical host.
func publicationPostURL(root *url.URL, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if raw == "" || u.User != nil || (u.IsAbs() && u.Host == "") || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid post URL: %q", raw)
	}
	u = root.ResolveReference(u)
	slug := strings.TrimPrefix(u.Path, "/p/")
	if u.Host == "" || !strings.HasPrefix(u.Path, "/p/") || slug == "" || strings.Contains(slug, "/") || slug == "." || slug == ".." {
		return "", fmt.Errorf("invalid post URL: %q", raw)
	}
	u.Scheme, u.Host, u.RawQuery, u.Fragment = root.Scheme, root.Host, "", ""
	return u.String(), nil
}
