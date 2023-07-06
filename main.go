package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
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

type Wrapper struct {
	Post Post `json:"post"`
}

func main() {
	pageUrl := "https://www.recomendo.com/p/finance-advicefind-the-best-productsounds-19-06-16"
	// fetch page HTML content
	res, err := http.Get(pageUrl)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	htmlContent, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatal(err)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlContent))
	if err != nil {
		log.Fatal(err)
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
		log.Fatal(err)
	}

	// Now convert the normal JSON string to a Go object
	var wrapper Wrapper
	err = json.Unmarshal([]byte(normalJSON), &wrapper)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(wrapper.Post)

	// startPrefix := "JSON.parse(" //"\"post\":" //
	// start := strings.Index(scriptContent, startPrefix) + len(startPrefix)
	// end := strings.LastIndex(scriptContent, ")")
	// jsonString := scriptContent[start : end-1]
	// print(jsonString)

	// jsonString, err = strconv.Unquote("\"" + jsonString + "\"")
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// var post map[string]Post
	// err = json.Unmarshal([]byte(jsonString), &post)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// fmt.Println("Title:", post["post"].Title)
	// fmt.Println("BodyHTML:", post["post"].BodyHTML)
}
