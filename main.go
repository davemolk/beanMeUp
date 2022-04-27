package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/PuerkitoBio/goquery"
)


func main() {
	beanUrl := "https://www.ranchogordo.com/collections/out-of-stock-beans"
	uAgent := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.88 Safari/537.36"

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", beanUrl, nil)
	if err != nil {
		log.Fatal("unable to set request", err)
	}
	req.Header.Set("User-Agent", uAgent)

	res, err := client.Do(req)
	if err != nil {
		log.Fatal("request failed", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		log.Fatalf("status code error: %d %s", res.StatusCode, res.Status)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal("error parsing Body", err)
	}

	doc.Find("div.sold-out").Each(func(i int, s *goquery.Selection) {
		name := s.Find("p.grid-link__title").First().Text()
		fmt.Println(name)
	})
}
