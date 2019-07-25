package app

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
)

// GitHubCredentials stores a GitHub username and password
type GitHubCredentials struct {
	Username string
	Password string
}

// LoginToGitHub logs into GitHub with the given
// credentials and returns an HTTTP client.
func LoginToGitHub(credentials GitHubCredentials) (httpClient *http.Client, err error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{
		Jar: jar,
	}
	csrf, err := GrabCSRFToken("https://github.com/login", &client)
	if err != nil {
		return nil, err
	}
	_, err = client.PostForm("https://github.com/session", url.Values{
		"authenticity_token": {csrf},
		"login":              {credentials.Username},
		"password":           {credentials.Password},
	})
	return &client, err
}

// GrabCSRFToken grabs the CSRF token from a GitHub page
func GrabCSRFToken(csrfURL string, client *http.Client) (token string, err error) {
	resp, err := client.Get(csrfURL)
	if err != nil {
		log.Println("Error getting CSRF token page.")
		log.Println(err)
	}
	re := regexp.MustCompile("authenticity_token\"\\svalue\\=\"([0-9A-z/=\\+]{32,})\"")
	data, err := ioutil.ReadAll(resp.Body)
	dataStr := string(data)
	match := re.FindStringSubmatch(dataStr)
	if len(match) == 2 {
		return match[1], err
	}
	return "", err
}

// DownloadRawFile downloads files from the githubusercontent CDN.
func DownloadRawFile(client *http.Client, base string, searchResult RepoSearchResult) (data []byte, err error) {
	fmt.Println(base + "/" + searchResult.Raw)
	resp, err := client.Get(base + "/" + searchResult.Raw)
	if err != nil {
		return nil, err
	}
	data, err = ioutil.ReadAll(resp.Body)
	return data, err
}

// RepoIsUnpopular uses stars/forks/watchers to determine the popularity of a repo.
func RepoIsUnpopular(client *http.Client, result RepoSearchResult) bool {
	resp, err := client.Get("https://github.com/" + result.Repo)
	if err != nil {
		log.Fatal(err)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	strData := string(data)
	regex := regexp.MustCompile("aria\\-label\\=\"(\\d+)\\suser(s?)\\sstarred\\sthis")
	match := regex.FindStringSubmatch(strData)
	if len(match) > 1 {
		stars, err := strconv.Atoi(match[1])
		if err != nil {
			log.Fatal(err)
		}
		if stars > 10 {
			return false
		}
	}
	return true
}
