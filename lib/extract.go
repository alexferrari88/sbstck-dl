package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Post struct {
	Id               int      `json:"id"`
	PublicationId    int      `json:"publication_id"`
	Type             string   `json:"type"`
	Slug             string   `json:"slug"`
	PostDate         string   `json:"post_date"`
	CanonicalUrl     string   `json:"canonical_url"`
	PreviousPostSlug string   `json:"previous_post_slug"`
	NextPostSlug     string   `json:"next_post_slug"`
	CoverImage       string   `json:"cover_image"`
	Description      string   `json:"description"`
	WordCount        int      `json:"wordcount"`
	PostTags         []string `json:"postTags"`
	Title            string   `json:"title"`
	BodyHTML         string   `json:"body_html"`
}

type PostWrapper struct {
	Post Post `json:"post"`
}

type Extractor struct {
	client *http.Client
}

func NewExtractor(c *http.Client) *Extractor {
	if c == nil {
		c = http.DefaultClient
	}
	return &Extractor{client: c}
}

func (e *Extractor) ExtractPost(ctx context.Context, pageUrl string) (Post, error) {
	// fetch page HTML content
	res, err := e.client.Get(pageUrl)
	if err != nil {
		return Post{}, err
	}
	defer res.Body.Close()
	htmlContent, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return Post{}, err

	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlContent))
	if err != nil {
		return Post{}, err

	}

	scriptContent := ""
	doc.Find("script").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if strings.Contains(s.Text(), "window._preloads") && strings.Contains(s.Text(), "JSON.parse(") {
			scriptContent = s.Text()
			return false
		}
		return true
	})

	start := strings.Index(scriptContent, "JSON.parse(\"") + len("JSON.parse(\"")
	end := strings.LastIndex(scriptContent, "\")")
	jsonString := scriptContent[start:end]

	// jsonString is a stringified JSON string. Convert it to a normal JSON string
	var normalJSON string
	err = json.Unmarshal([]byte("\""+jsonString+"\""), &normalJSON)
	if err != nil {
		return Post{}, err

	}

	// Now convert the normal JSON string to a Go object
	var wrapper PostWrapper
	err = json.Unmarshal([]byte(normalJSON), &wrapper)
	if err != nil {
		return Post{}, err

	}

	return wrapper.Post, nil
}

func (e *Extractor) GetAllPostsURLs(ctx context.Context, pubUrl string) ([]string, error) {
	// fetch the sitemap of the publication
	res, err := e.client.Get(pubUrl + "/sitemap.xml")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	sitemapContent, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	// the sitemap is an XML file with a list of URLs
	// we are interested in the <loc> tags only if the URL contains "/p/"
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(sitemapContent))
	if err != nil {
		return nil, err
	}

	urls := []string{}
	doc.Find("loc").Each(func(i int, s *goquery.Selection) {
		url := s.Text()
		if strings.Contains(url, "/p/") {
			urls = append(urls, url)
		}
	})

	return urls, nil
}

func (e *Extractor) ExtractAllPosts(ctx context.Context, pubUrl string) ([]Post, error) {
	urls, err := e.GetAllPostsURLs(ctx, pubUrl)
	if err != nil {
		return nil, err
	}

	posts := []Post{}
	for _, url := range urls {
		post, err := e.ExtractPost(ctx, url)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	return posts, nil
}
