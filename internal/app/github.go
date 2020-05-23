package app

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/github"
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
	resp, err := client.PostForm("https://github.com/session", url.Values{
		"authenticity_token": {csrf},
		"login":              {credentials.Username},
		"password":           {credentials.Password},
	})
	// fmt.Println(resp.StatusCode)
	data, err := ioutil.ReadAll(resp.Body)
	dataStr := string(data)
	if strings.Index(dataStr, "name=\"otp\"") > -1 {
		csrf, err = GrabCSRFTokenBody(dataStr)
		if err != nil {
			return nil, err
		}

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
		// fmt.Println(string(data))
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
	re := regexp.MustCompile("authenticity_token\"\\svalue\\=\"([0-9A-z/=\\+]{32,})\"")
	match := re.FindStringSubmatch(pageBody)
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
	sb.WriteString("?q=" + url.QueryEscape(query))
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
