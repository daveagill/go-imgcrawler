package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gomodule/redigo/redis"

	"github.com/daveagill/go-imgcrawler/crawler"
)

func main() {
	var (
		url          string
		redisAddr    string
		redisNetwork string
		workersN     int
	)

	flag.StringVar(&url, "url", "", "Required. The seed URL to crawl from")
	flag.StringVar(&redisAddr, "redisAddr", "", "Required. The redis host address and port")
	flag.StringVar(&redisNetwork, "redisNetwork", "tcp", "The redis network")
	flag.IntVar(&workersN, "workers", 1, "The number of concurrent workers")
	flag.Parse()

	if url == "" {
		fmt.Fprintln(os.Stderr, "-url parameter is required")
		os.Exit(2)
	}

	if redisAddr == "" {
		fmt.Fprintln(os.Stderr, "-redisAddr parameter is required")
		os.Exit(2)
	}

	// create Redis connection pool
	pool := &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.Dial(redisNetwork, redisAddr)
		},
	}
	defer pool.Close()

	// perform the crawling
	c := crawler.New(pool)
	c.Seed(url)
	c.RunN(workersN)

	// report some information about the crawl (URLs visited and <img> tags encountered)
	imgSrcs, _ := redis.Strings(pool.Get().Do("SMEMBERS", c.KeyImageSrcs))
	hrefs, _ := redis.Strings(pool.Get().Do("SMEMBERS", c.KeyVisitedHREFs))

	fmt.Println("Crawling Complete")
	fmt.Println("Visisted HREFS:", hrefs)
	fmt.Println("Found Images:", imgSrcs)
}
