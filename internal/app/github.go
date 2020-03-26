package app

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/github"
)

// GitHubCredentials stores a GitHub username and password
type GitHubCredentials struct {
	Username string
	Password string
}

// SearchOptions are the options that the GitHub search will use.
type SearchOptions struct {
	MaxPages int
	Language string
	github.SearchOptions
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
	rt := WithHeader(client.Transport)
	rt.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.132 Safari/537.36")
	client.Transport = rt
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
	resp, err := client.Get(base + "/" + searchResult.Raw)
	if err != nil {
		return nil, err
	}
	data, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
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
	resp.Body.Close()
	strData := string(data)
	regex := regexp.MustCompile("aria\\-label\\=\"(\\d+)\\suser(s?)\\sstarred\\sthis")
	match := regex.FindStringSubmatch(strData)
	if len(match) > 1 {
		stars, err := strconv.Atoi(match[1])
		if err != nil {
			log.Fatal(err)
		}
		if stars > 6 {
			return false
		}
	}
	return true
}

// GetRawGistPage gets the source code for a Gist.
func GetRawGistPage(client *http.Client, gist string) string {
	resp, err := client.Get("https://gist.github.com/" + gist)
	if err != nil {
		log.Fatal(err)
	}
	escaped := regexp.QuoteMeta(gist)
	regex := regexp.MustCompile("href\\=\"\\/(" + escaped + "\\/raw\\/[0-9a-z]{40}\\/[\\w_\\-\\.\\/\\%]{1,255})\"\\>")
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	resp.Body.Close()
	match := regex.FindStringSubmatch(string(body))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

// ConstructSearchURL serializes its parameters into a search URL
func ConstructSearchURL(base string, query string, options SearchOptions) string {
	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("?q=" + url.QueryEscape("\""+query+"\" stars:<5 fork:false"))
	sb.WriteString("&p=" + strconv.Itoa(options.Page))
	// sb.WriteString("&o=desc")    // + options.Order)
	sb.WriteString("&s=indexed") // + options.Sort)
	sb.WriteString("&l=" + options.Language)
	sb.WriteString("&type=Code")
	return sb.String()
}

type withHeader struct {
	http.Header
	rt http.RoundTripper
}

func WithHeader(rt http.RoundTripper) withHeader {
	if rt == nil {
		rt = http.DefaultTransport
	}

	return withHeader{Header: make(http.Header), rt: rt}
}

func (h withHeader) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.Header {
		req.Header[k] = v
	}

	return h.rt.RoundTrip(req)
}
