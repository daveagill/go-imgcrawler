package crawler

import (
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/gomodule/redigo/redis"

	"github.com/PuerkitoBio/purell"

	neturl "net/url"
)

// Crawler holds config to configure web scraping behaviour
type Crawler struct {
	RedisPool        *redis.Pool
	KeyActiveWorkers string
	KeyCrawlQ        string
	KeyVisitedHREFs  string
	KeyImageSrcs     string
}

// New allocates a new Crawler with default config
func New(p *redis.Pool) *Crawler {
	return &Crawler{
		RedisPool:        p,
		KeyActiveWorkers: "activeWorkers",
		KeyCrawlQ:        "crawlQ",
		KeyVisitedHREFs:  "visitedHREFs",
		KeyImageSrcs:     "imageSrcs",
	}
}

// Seed adds a URL to the crawl queue
func (c *Crawler) Seed(url string) {
	conn := c.RedisPool.Get()
	conn.Do("SADD", c.KeyCrawlQ, url)
	conn.Close()
}

// RunN starts 'n' concurrent crawlers and blocks until completion
func (c *Crawler) RunN(n int) {
	wg := sync.WaitGroup{}
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			c.Run()
			wg.Done()
		}()
	}

	wg.Wait()
}

// Run starts a single-threaded crawler and blocks until completion
func (c *Crawler) Run() {
	conn := c.RedisPool.Get()
	defer conn.Close()

	for {
		// we are active
		_, err := conn.Do("INCR", c.KeyActiveWorkers)
		if err != nil {
			log.Println(err)
			return
		}

		c.crawl(conn)

		// we are no longer active
		active, err := redis.Int(conn.Do("DECR", c.KeyActiveWorkers))
		if err != nil {
			log.Println(err)
			return
		}

		// wait to see if the queue fills up again...
		for {
			// if no more workers then exit
			if active == 0 {
				return
			}

			// wait a moment
			time.Sleep(1 * time.Second)

			// check the queue, wake up again if no longer empty
			qLen, _ := redis.Int(conn.Do("SCARD", c.KeyCrawlQ))
			if qLen > 0 {
				break // break out of the spinlock to continue crawling
			}

			// still empty, re-check number of active workers
			active, _ = redis.Int(conn.Do("GET", c.KeyActiveWorkers))
		}
	}
}

func (c *Crawler) crawl(conn redis.Conn) {
	for {
		// grab the next URL to crawl
		url, err := redis.String(conn.Do("SPOP", c.KeyCrawlQ))
		if err != nil {
			// exit only once queue is empty
			if err == redis.ErrNil {
				return
			}

			log.Println(err)
			continue
		}

		// record as visited
		inserted, err := redis.Int(conn.Do("SADD", c.KeyVisitedHREFs, url))
		if err != nil {
			log.Println(err)
			continue
		}

		// skip if already visited
		if inserted == 0 {
			continue
		}

		// scrape the page
		log.Println("Crawling:", url)
		hrefs, imgSrcs := scrape(url)

		// push to Redis
		for _, url := range imgSrcs {
			conn.Send("SADD", c.KeyImageSrcs, url)
		}
		for _, url := range hrefs {
			conn.Send("SADD", c.KeyCrawlQ, url)
		}
		conn.Flush()
	}
}

func scrape(url string) (hrefs []string, imgSrcs []string) {
	// request the page
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	// skip if not HTML
	ct := resp.Header.Get("content-type")
	if !strings.HasPrefix(ct, "text/html") {
		log.Println("Skipping non-HTML page:", url, " with content-type:", ct)
		return []string{}, []string{}
	}

	// extract urls
	imgSrcs, hrefs = parse(resp.Body)
	imgSrcs = resolveURLs(url, imgSrcs, false)
	hrefs = resolveURLs(url, hrefs, true)

	return hrefs, imgSrcs
}

func resolveURLs(base string, urls []string, skipExternal bool) []string {
	baseURL, err := neturl.Parse(base)
	if err != nil {
		return []string{}
	}

	baseHost := baseURL.Hostname()
	absUrls := []string{}

	for _, url := range urls {
		parsed, err := neturl.Parse(url)

		// skip invalid URLs
		if err != nil {
			continue
		}

		// convert to absolute URL
		absolute := baseURL.ResolveReference(parsed)

		// (optionally) skip URLs external to the base domain
		if skipExternal && absolute.Hostname() != baseHost {
			continue
		}

		absUrls = append(absUrls, toSanitizedString(absolute))
	}

	return absUrls
}

func toSanitizedString(u *neturl.URL) string {
	flags := purell.FlagsUsuallySafeGreedy | purell.FlagRemoveFragment | purell.FlagRemoveDuplicateSlashes | purell.FlagSortQuery
	return purell.NormalizeURL(u, flags)
}

func parse(r io.Reader) (imgSrcs []string, hrefs []string) {
	tokens := html.NewTokenizer(r)
	imgSrcs = []string{}
	hrefs = []string{}

	for {
		tokType := tokens.Next()

		if tokType == html.ErrorToken {
			break
		}

		if tokType == html.StartTagToken || tokType == html.SelfClosingTagToken {
			tok := tokens.Token()

			isImg, src := matchTag(&tok, "img", "src")
			if isImg {
				imgSrcs = append(imgSrcs, src)
			}

			isAnchor, href := matchTag(&tok, "a", "href")
			if isAnchor {
				hrefs = append(hrefs, href)
			}
		}
	}

	return imgSrcs, hrefs
}

func matchTag(tok *html.Token, tag string, attrName string) (isMatch bool, val string) {
	isMatch = tok.Data == tag
	if isMatch {
		for _, attr := range tok.Attr {
			if attr.Key == attrName {
				val = attr.Val
				break
			}
		}
	}

	return isMatch, val
}
