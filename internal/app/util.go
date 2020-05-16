package app

import (
	"io/ioutil"
	"log"
	"strings"

	"github.com/fatih/color"
)

// GetFileLines takes a file path and returns its lines, stringified.
func GetFileLines(file string) (lines []string) {
	dat, err := ioutil.ReadFile(file)
	if err != nil {
		color.Red("[!] File '" + file + "' does not exist.")
		log.Fatal()
	}
	linesUnparsed := strings.Split(string(dat), "\n")
	for _, line := range linesUnparsed {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// Abs returns the absolute value of x.
func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// CheckErr checks if an error is not null, and
// exits if it is not null.
func CheckErr(err error) {
	if err != nil {
		color.Red("[!] An error has occurred.")
		log.Fatal(err)
	}
}

// GetRepoURLForSearchResult returns the URL of the repo depending on
// RepoSearchResult source
func GetRepoURLForSearchResult(repo RepoSearchResult) string {
	if repo.Source == "repo" {
		return "https://github.com/" + repo.Repo
	} else if repo.Source == "gist" {
		return "https://gist.github.com/" + repo.Repo
	}
	// Left this way in case other Source values ever exist
	return ""
}

// GetRawURLForSearchResult returns a raw data URL for a RepoSearchResult
func GetRawURLForSearchResult(repo RepoSearchResult) string {
	if repo.Source == "repo" {
		return "https://raw.githubusercontent.com"
	} else if repo.Source == "gist" {
		return "https://gist.githubusercontent.com"
	}
	// Left this way in case other Source values ever exist
	return ""
}
