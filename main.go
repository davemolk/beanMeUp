package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	s "strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/joho/godotenv"
)

type Beans map[string]bool

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

	// scrape phase
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal("error parsing Body", err)
	}

	today := Beans{}

	doc.Find("div.sold-out").Each(func(i int, s *goquery.Selection) {
		name := s.Find("p.grid-link__title").First().Text()
		if name == "" {
			message := []byte("issue with scrape -- please check selectors")
			email(message)
		}
		today[name] = true
	})

	

	// compare
	yesterday := Beans{
		"Black Garbanzo Bean":      true,
		"Very tasty bean": true,
		"Cassoulet (Tarbais) Bean": true,
		"Chiapas Black Bean":       true,
		"The best": true,
	}

	available := []string{}

	for name := range yesterday {
		if _, ok := today[name]; !ok {
			available = append(available, name)
		}
	}

	fmt.Println(available)

	// convert to json for upload
	js, err := json.MarshalIndent(today, "", "\t")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(js))

	// email results [CURRENTLY NOT THE CORRECT DATA]
	message := []byte(string(js))
	email(message)

	// get yesterday data
	storage := Beans{}
	if err := json.Unmarshal(js, &storage); err != nil {
		log.Fatal("cannot unmarshal", err)
	}
	fmt.Println("********************************")
	fmt.Println(storage)
	

	
}

func email(message []byte) {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("Error loading .env", err)
	}

	from := os.Getenv("FROM")
	password := os.Getenv("PASSWORD")
	to := []string{
		os.Getenv("TO"),
	}
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")

	body := message

	auth := smtp.PlainAuth("", from, password, smtpHost)

	addr := s.Join([]string{smtpHost, smtpPort}, ":")

	emailErr := smtp.SendMail(addr, auth, from,to, body)
	if emailErr != nil {
		log.Println("unable to send email", emailErr)
		return
	}
	log.Printf("Email sent to %s", to[0])
}
/*

package main

import (
	"fmt"
)

type Beans map[string]bool

func main() {
	old := Beans{
		"Black Garbanzo Bean":      true,
		"Cassoulet (Tarbais) Bean": true,
	}
	new := []string{"Black Garbanzo Bean", "Cassoulet (Tarbais) Bean", "Chiapas Black Bean"}

	result := []string{}

	for _, v := range new {
		if _, ok := old[v]; !ok {
			result = append(result, v)
		}
	}
	fmt.Println(result)

}
*/