package app

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/waigani/diffparser"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

var queue []RepoSearchResult
var reposStored = 0
var finishedRepos []string
var pool = make(chan bool, 10)
var poolInitialized = false

// Dig into the secrets of a repo
func Dig(result RepoSearchResult) (matches []Match) {
	if !poolInitialized {
		pool = make(chan bool, GetFlags().Threads)
		poolInitialized = true
	}
	matchChannel := make(chan []Match)
	pool <- true
	go func() {
		matchChannel <- digHelper(result)
		<-pool
		close(matchChannel)
	}()
	matches = <-matchChannel
	return matches
}

func digHelper(result RepoSearchResult) (matches []Match) {
	if GetFlags().Debug {
		fmt.Println("Digging " + result.Repo)
	}
	var repo *git.Repository
	disk := false
	var err error
	if _, err = os.Stat("/tmp/githound/" + result.Repo); os.IsNotExist(err) {
		for {
			context, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			repo, err = git.PlainCloneContext(context, "/tmp/githound/"+result.Repo, false, &git.CloneOptions{
				URL:          "https://github.com/" + result.Repo,
				SingleBranch: true,
				Depth:        20,
			})
			if err != nil {
				if GetFlags().Debug {
					fmt.Println(err)
				}
				return
			}

			if err != nil {
				if GetFlags().Debug {
					if disk {
						fmt.Println("Error opening repo from disk: " + result.Repo)
					} else {
						fmt.Println("Error cloning repo: " + result.Repo)
					}
					fmt.Println(err)
				}
				return matches
			}
			if GetFlags().Debug {
				fmt.Println("Finished cloning " + result.Repo)
			}
			reposStored++
			if reposStored%20 == 0 {
				size, err := DirSize("/tmp/githound")
				if err != nil {
					log.Fatal(err)
				}
				if size > 50e+6 {
					ClearFinishedRepos()
				}
			}
			ref, err := repo.Head()
			if err != nil {
				if GetFlags().Debug {
					fmt.Println("Error accessing repo head: " + result.Repo)
					fmt.Println(err)
				}
				return matches
			}

			matchMap := make(map[Match]bool)
			if GetFlags().DigRepo {
				// search current repo state
				root := "/tmp/githound/" + result.Repo
				var files []string
				err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
					files = append(files, path)
					return nil
				})
				if err != nil {
					fmt.Println(err)
				}
				for _, file := range files {
					data, err := ioutil.ReadFile(file)
					if err == nil {
						var ascii []byte
						for _, b := range data {
							if 0 < b && b < 127 {
								ascii = append(ascii, b)
							}
						}
						if float32(len(ascii))/float32(len(data)) < 0.7 {
							// fmt.Println("skipping " + file)
							continue
						}
						newMatches, score := GetMatchesForString(string(ascii), result)
						if score > 0 {
							for _, match := range newMatches {
								relPath := strings.Join(strings.Split(file[len("/tmp/githound/"):], "/")[2:], "/")
								match.CommitFile = relPath
								match.File = relPath
								if !matchMap[match] {
									matchMap[match] = true
									matches = append(matches, match)
								}
							}
						}
					} else {
						// fmt.Println(err)
					}
				}
			}

			var waitGroup sync.WaitGroup
			if GetFlags().DigCommits {
				commit, err := repo.CommitObject(ref.Hash())
				if err != nil {
					if GetFlags().Debug {
						fmt.Println("Error getting commit object: " + result.Repo)
						fmt.Println(err)
					}
					return matches
				}

				commitIter, err := repo.Log(&git.LogOptions{From: commit.Hash})

				lastHash, err := commit.Tree()
				if err != nil {
					log.Fatal(err)
				}

				number := 0
				commitIter.ForEach(
					func(c *object.Commit) error {
						if number > 30 {
							return nil
						}
						number++
						commitTree, err := c.Tree()
						if err != nil {
							return err
						}
						fmt.Println("scanning diff")
						diffMatches := ScanDiff(lastHash, commitTree, result)
						for _, match := range diffMatches {
							match.Commit = c.Hash.String()
							if !matchMap[match] {
								matchMap[match] = true
								matches = append(matches, match)
							}
						}
						lastHash = commitTree
						return nil
					})
				if err != nil {
					log.Println(err)
				}
			}
			waitGroup.Wait()
			// fmt.Println("finished scanning repo " + result.Repo)
			finishedRepos = append(finishedRepos, result.Repo)
			if GetFlags().Debug {
				fmt.Println("Finished scanning repo " + result.Repo)
			}
			return matches

		}
	} else {
		return
	}
}

// ScanDiff finds secrets in the diff between two Git trees.
func ScanDiff(from *object.Tree, to *object.Tree, result RepoSearchResult) (matches []Match) {
	if from == to || from == nil || to == nil {
		return matches
	}
	diff, err := from.Diff(to)
	if err != nil {
		log.Fatal(err)
	}
	for _, change := range diff {
		if change == nil {
			continue
		}
		patch, err := change.Patch()
		if err != nil {
			if GetFlags().Debug {
				fmt.Println("Diff scan error: Patch error.")
				fmt.Println(err)
			}
			continue
		}
		patchStr := patch.String()
		diffData, err := diffparser.Parse(patchStr)
		if err != nil {
			log.Fatal(err)
		}
		matches, _ = GetMatchesForString(patchStr, result)
		for _, diffFile := range diffData.Files {
			for _, match := range MatchFileExtensions(diffFile.NewName, result) {
				matches = append(matches, match)
			}
		}
	}
	return matches
}

// DirSize gets the size of a diretory.
func DirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

// ClearFinishedRepos deletes the stored repos that have already been analyzed.
func ClearFinishedRepos() {
	for _, repoString := range finishedRepos {
		os.RemoveAll("/tmp/githound/" + repoString)
	}
	finishedRepos = nil
}

// ClearRepoStorage deletes all stored repos from the disk.
func ClearRepoStorage() {
	os.RemoveAll("/tmp/githound")
}
