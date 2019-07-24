package app

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/github"
)

type SearchOptions struct {
	MaxPages int
	github.SearchOptions
}

type RepoSearchResult struct {
	Repo   string
	File   string
	Raw    string
	Source string
	Query  string
}

// Search Everything
func Search(query string, args []string, client *http.Client) (results []RepoSearchResult, err error) {

	options := SearchOptions{
		MaxPages: 100,
	}

	resultMap := make(map[string]bool)
	err = SearchGitHub(query, options, client, &results, resultMap)
	if err != nil {

	}
	// SearchGist(query, options, client)
	return results, err
}

// SearchGitHub searches GitHub code results for the given query
func SearchGitHub(query string, options SearchOptions, client *http.Client, results *[]RepoSearchResult, resultSet map[string]bool) (err error) {
	// TODO: A lot of this code is shared between GitHub and Gist searches,
	// so we should rework the logic
	base := "https://github.com/search"
	page, pages := 0, 1
	var delay = 5
	for page < pages {
		options.Page = (page + 1)
		response, err := client.Get(ConstructSearchURL(base, query, options))
		if err != nil {
			fmt.Println("ERROR")
			if response != nil {
				if response.StatusCode == 403 {
					delay += 5
					fmt.Println("Rate limited by GitHub. Waiting " + strconv.Itoa(delay) + "s...")
					time.Sleep(time.Duration(delay) * time.Second)
				} else if response.StatusCode == 503 {
					fmt.Println("503 breaking")
					break
				}
			} else {
				fmt.Println(err)
			}
			continue
		}
		if delay > 10 {
			delay--
		}
		responseData, err := ioutil.ReadAll(response.Body)
		responseStr := string(responseData)
		if err != nil {
			log.Fatal(err)
		}
		if page == 0 {
			regex := regexp.MustCompile("\\bdata\\-total\\-pages\\=\"(\\d+)\"")
			match := regex.FindStringSubmatch(responseStr)
			if err != nil {
				log.Fatal(err)
			}
			if len(match) == 2 {
				newPages, err := strconv.Atoi(match[1])
				if err == nil {
					pages = newPages
					if pages > 99 {
						fmt.Println("[*] Searching 100+ pages of results...")
					} else {
						fmt.Println("[*] Searching 100 pages of results...")
					}
				} else {
					fmt.Println("An error occurred while parsing the page count.")
					fmt.Println(err)
				}
			} else {
				fmt.Println("[*] Searching 1 page of results...")
			}

		}
		page++
		resultRegex := regexp.MustCompile("href=\"(\\/(.*)\\/blob\\/[0-9a-f]{40}\\/([^#\"]+))\">")
		matches := resultRegex.FindAllStringSubmatch(responseStr, -1)
		for _, element := range matches {
			if len(element) == 4 {
				if resultSet[(element[2]+"/"+element[3])] == true {
					continue
				}
				resultSet[(element[2] + "/" + element[3])] = true
				go ScanAndPrintResult(client, RepoSearchResult{
					Repo:   element[2],
					File:   element[3],
					Raw:    element[1],
					Source: "repo",
					Query:  query,
				})
			} else {
				fmt.Println(results)
			}
		}
		time.Sleep(time.Duration(delay) * time.Second)
	}
	return nil
}

// SearchGist searches Gist results for the given query
func SearchGist(query string, options SearchOptions, client *http.Client) {

}

// ConstructSearchURL serializes its parameters into a search URL
func ConstructSearchURL(base string, query string, options SearchOptions) string {
	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("?q=" + url.QueryEscape("\""+query+"\""))
	sb.WriteString("&p=" + strconv.Itoa(options.Page))
	sb.WriteString("&o=" + options.Order)
	sb.WriteString("&s=" + options.Sort)
	sb.WriteString("&type=Code")
	return sb.String()
}
