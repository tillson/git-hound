package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v57/github"
	"github.com/pquerna/otp/totp"
)

// GitHubCredentials stores a GitHub username and password
type GitHubCredentials struct {
	Username string
	Password string
	OTP      string
}

// SearchOptions are the options that the GitHub search will use.
type SearchOptions struct {
	MaxPages int
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
	resp, err := client.PostForm("https://github.com/session", url.Values{
		"authenticity_token": {csrf},
		"login":              {credentials.Username},
		"password":           {credentials.Password},
	})
	// fmt.Println(resp.StatusCode)
	data, err := ioutil.ReadAll(resp.Body)
	dataStr := string(data)
	// fmt.Println(dataStr)
	if strings.Index(dataStr, "Incorrect username or password.") > -1 {
		return nil, fmt.Errorf("Incorrect username or password.")
	}
	if strings.Index(dataStr, "app_otp") > -1 {
		csrf, err = GrabCSRFTokenBody(dataStr)
		if err != nil {
			return nil, err
		}
		// fmt.Println(csrf)
		otp := HandleOTPCode(credentials)

		if strings.Index(resp.Request.URL.String(), "verified-device") > -1 {
			resp, err = client.PostForm("https://github.com/sessions/verified-device", url.Values{

				"authenticity_token": {csrf},
				"otp":                {otp},
			})
			data, err = ioutil.ReadAll(resp.Body)
		} else {
			resp, err = client.PostForm("https://github.com/sessions/two-factor", url.Values{

				"authenticity_token": {csrf},
				"otp":                {otp},
			})
			data, err = ioutil.ReadAll(resp.Body)
		}
	}

	return &client, err
}

// HandleOTPCode returns a user's OTP code for authenticating with Github by searching
// config values, then CLI arguments, then prompting the user for input
func HandleOTPCode(credentials GitHubCredentials) string {
	var otp string
	if credentials.OTP != "" {
		// Generate a TOTP code based on TOTP seed in config
		otp, _ = totp.GenerateCode(credentials.OTP, time.Now())
	} else if GetFlags().OTPCode != "" {
		// Use the provided CLI argument (--otp-code) for OTP code
		otp = GetFlags().OTPCode
	} else {
		// Prompt the user for OTP code
		tty, err := os.Open("/dev/tty")
		if err != nil {
			log.Fatalf("can't open /dev/tty: %s", err)
		}
		fmt.Printf("Enter your GitHub 2FA code: ")
		scanner := bufio.NewScanner(tty)
		_ = scanner.Scan()
		otp = scanner.Text()
	}
	return otp
}

// GrabCSRFToken grabs the CSRF token from a GitHub page
func GrabCSRFToken(csrfURL string, client *http.Client) (token string, err error) {
	resp, err := client.Get(csrfURL)
	if err != nil {
		log.Println("Error getting CSRF token page.")
		log.Println(err)
	}
	data, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	dataStr := string(data)
	return GrabCSRFTokenBody(dataStr)
}

// GrabCSRFTokenBody grabs the CSRF token from a GitHub page
func GrabCSRFTokenBody(pageBody string) (token string, err error) {
	re := regexp.MustCompile("authenticity_token\"\\svalue\\=\"([0-9A-z/=\\+\\-_]{32,})\"")
	match := re.FindStringSubmatch(pageBody)
	if len(match) == 2 {
		return match[1], err
	}
	return "", err
}

// DownloadRawFile downloads files from the githubusercontent CDN.
func DownloadRawFile(client *http.Client, base string, searchResult RepoSearchResult) (data []byte, err error) {
	// If the raw URL contains '%' character, gracefully skip it
	if strings.Contains(searchResult.Raw, "%") {
		// Return empty byte array with nil error to gracefully skip this file
		return []byte{}, nil
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Split the path and encode each component
	pathParts := strings.Split(searchResult.Raw, "/")
	encodedParts := make([]string, len(pathParts))
	for i, part := range pathParts {
		encodedParts[i] = url.PathEscape(part)
	}
	encodedPath := strings.Join(encodedParts, "/")

	// Construct the full URL with encoded path
	fullURL := base + "/" + encodedPath

	// Create a request with the timeout context
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	TrackAPIRequest("GitHub Web", fmt.Sprintf("GET %s (download raw file)", fullURL))

	// Perform the GET request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check response code
	if resp.StatusCode >= 400 {
		fmt.Println(fullURL)
		return []byte{}, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	// Read the response body with a size limit to prevent memory explosions
	// Limit to 10MB max file size
	const maxSize = 10 * 1024 * 1024
	return ioutil.ReadAll(io.LimitReader(resp.Body, maxSize))
}

// Cache for repository popularity to avoid repeated HTTP requests
var repoPopularityCache = make(map[string]bool)
var repoCacheMutex sync.RWMutex

// RepoIsUnpopular uses stars/forks/watchers to determine the popularity of a repo.
func RepoIsUnpopular(client *http.Client, result RepoSearchResult) bool {
	// Check cache first
	repoCacheMutex.RLock()
	if isUnpopular, exists := repoPopularityCache[result.Repo]; exists {
		repoCacheMutex.RUnlock()
		return isUnpopular
	}
	repoCacheMutex.RUnlock()

	// Default timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a request with timeout context
	req, err := http.NewRequestWithContext(ctx, "GET", "https://github.com/"+result.Repo, nil)
	if err != nil {
		// If we can't create the request, assume unpopular to be safe
		return true
	}

	TrackAPIRequest("GitHub Web", fmt.Sprintf("GET https://github.com/%s (check popularity)", result.Repo))
	resp, err := client.Do(req)
	if err != nil {
		// Network error, assume unpopular to be safe
		return true
	}
	defer resp.Body.Close()

	// Only read a limited amount of the response
	bodyBytes, err := ioutil.ReadAll(io.LimitReader(resp.Body, 100*1024)) // Limit to 100KB
	if err != nil {
		return true
	}

	// Convert to string for regex search
	strData := string(bodyBytes)

	// Parse star count
	regex := regexp.MustCompile("aria\\-label\\=\"(\\d+)\\suser(s?)\\sstarred\\sthis")
	match := regex.FindStringSubmatch(strData)

	isUnpopular := true
	if len(match) > 1 {
		stars, err := strconv.Atoi(match[1])
		if err == nil && stars > 6 {
			isUnpopular = false
		}
	}

	// Cache the result
	repoCacheMutex.Lock()
	repoPopularityCache[result.Repo] = isUnpopular
	repoCacheMutex.Unlock()

	return isUnpopular
}

// GetRawGistPage gets the source code for a Gist.
func GetRawGistPage(client *http.Client, gist string) string {
	TrackAPIRequest("GitHub Web", fmt.Sprintf("GET https://gist.github.com/%s (get gist)", gist))
	resp, err := client.Get("https://gist.github.com/" + gist)
	if err != nil {
		log.Fatal(err)
	}
	escaped := regexp.QuoteMeta(gist)
	regex := regexp.MustCompile("href\\=\"\\/(" + escaped + "\\/raw\\/[0-9a-z]{40}\\/[\\w_\\-\\.\\/\\%]{1,255})\"")
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
	sb.WriteString("?q=" + url.QueryEscape(query))
	sb.WriteString("&p=" + strconv.Itoa(options.Page))
	// sb.WriteString("&o=desc")    // + options.Order)
	sb.WriteString("&s=indexed") // + options.Sort)
	sb.WriteString("&type=code")
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
