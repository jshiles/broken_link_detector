package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"

	"golang.org/x/net/html"
)

// Link represents a hyperlink with its URL and HTTP status.
type Link struct {
	URL    string
	Status int
}

// WebPage represents a webpage with its URL and a list of extracted links.
type WebPage struct {
	URL   string
	Links []Link
}

func main() {
	// Define flags
	rootURL := flag.String("url", "", "The starting URL for crawling")
	maxDepth := flag.Int("depth", 1, "The maximum depth for crawling")
	verbose := flag.Bool("verbose", false, "Controls the logging to the screen")
	flag.Parse()

	// Validate flags
	if *rootURL == "" {
		log.Fatal("The -url flag is required.")
	}
	if *maxDepth < 0 {
		log.Fatal("The -depth flag must be a non-negative integer.")
	}

	visited := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	log.Printf("Starting crawl at: %s, Depth: %d\n", *rootURL, *maxDepth)
	crawl(*rootURL, *maxDepth, 0, *verbose, &visited, &mu, &wg)

	wg.Wait() // Wait for all goroutines to finish
}

// crawl recursively fetches pages up to a specified depth and processes their links.
func crawl(pageURL string, maxDepth, currentDepth int, verbose bool, visited *map[string]bool, mu *sync.Mutex, wg *sync.WaitGroup) {
	if currentDepth > maxDepth {
		return
	}

	mu.Lock()
	if (*visited)[pageURL] {
		mu.Unlock()
		return
	}
	(*visited)[pageURL] = true
	mu.Unlock()

	wg.Add(1)
	go func() {
		defer wg.Done()

		webPage, err := fetchWebPage(pageURL)
		if err != nil {
			log.Printf("Error fetching page %s: %v", pageURL, err)
			return
		}

		log.Printf("Fetched: %s, Depth: %d", webPage.URL, currentDepth)
		brokenLinks := fetchURLsConcurrently(webPage.Links, verbose)
		log.Printf("Broken Links on %s:", webPage.URL)
		for _, link := range brokenLinks {
			log.Printf("- %s (Status: %d)", link.URL, link.Status)
		}

		// Recursively process child links
		for _, link := range webPage.Links {
			absoluteURL, err := resolveURL(pageURL, link.URL)
			if err == nil {
				crawl(absoluteURL, maxDepth, currentDepth+1, verbose, visited, mu, wg)
			}
		}
	}()
}

// fetchWebPage fetches a webpage by URL and extracts its links.
func fetchWebPage(pageURL string) (*WebPage, error) {
	resp, err := http.Get(pageURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %s", resp.Status)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing HTML: %w", err)
	}

	links := extractLinks(doc)
	return &WebPage{URL: pageURL, Links: links}, nil
}

// extractLinks traverses an HTML document and returns all hyperlinks as a list of Links.
func extractLinks(n *html.Node) []Link {
	var links []Link
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					links = append(links, Link{URL: attr.Val})
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return links
}

// resolveURL converts a relative URL to an absolute one based on the base URL.
func resolveURL(base, href string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	absoluteURL, err := baseURL.Parse(href)
	if err != nil {
		return "", err
	}
	return absoluteURL.String(), nil
}

// fetchURLsConcurrently fetches the URLs concurrently and returns those
// that respond with HTTP status 400 or 500 errors.
func fetchURLsConcurrently(links []Link, verbose bool) []Link {
	var brokenLinks []Link
	var wg sync.WaitGroup
	ch := make(chan Link)

	for _, link := range links {
		wg.Add(1)
		go func(link Link) {
			defer wg.Done()
			if verbose {
				log.Printf("Fetching URL: %s", link.URL)
			}
			link.fetchStatus()
			if link.Status >= 400 && link.Status < 600 {
				log.Printf("Broken URL: %s, Status: %d", link.URL, link.Status)
				ch <- link
			} else {
				log.Printf("Valid URL: %s, Status: %d", link.URL, link.Status)
			}
		}(link)
	}

	// Close the channel once all goroutines complete
	go func() {
		wg.Wait()
		close(ch)
	}()

	// Collect broken links from the channel
	for link := range ch {
		brokenLinks = append(brokenLinks, link)
	}

	return brokenLinks
}

// fetchStatus fetches the HTTP status of the Link and updates its Status field.
func (l *Link) fetchStatus() {
	resp, err := http.Get(l.URL)
	if err != nil {
		log.Printf("Error fetching URL: %s, Error: %v", l.URL, err)
		l.Status = 0 // Use 0 to indicate an error
		return
	}
	defer resp.Body.Close()
	l.Status = resp.StatusCode
}
