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
