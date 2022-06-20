package app

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/waigani/diffparser"
)

var queue []RepoSearchResult
var reposStored = 0
var finishedRepos []string
var pool = make(chan bool, 10)
var poolInitialized = false

// Dig into the secrets of a repo
func Dig(result RepoSearchResult) (matches []Match) {
	// fmt.Println(result.Repo)
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
					if strings.HasPrefix(path, root+"/.git/") {
						return nil
					}
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
						fileResult := result
						fileResult.File = file
						score := 0
						var newMatches []Match
						for _, match := range MatchFileExtensions(file, fileResult) {
							newMatches = append(newMatches, match)
							score += 5
						}
						if float32(len(ascii))/float32(len(data)) < 0.7 {
							// fmt.Println("skipping: " + file)
						} else {
							searchMatches, searchScore := GetMatchesForString(string(ascii), result)
							score += searchScore
							// fmt.Println(searchMatches)
							if searchScore > 1 {
								// fmt.Println(searchMatches)
								for _, newMatch := range searchMatches {
									newMatches = append(newMatches, newMatch)
								}
							}
						}
						// fmt.Println(file)
						if score > 1 {
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
				if err != nil {
					fmt.Println(err)
					continue
				}
				lastHash, err := commit.Tree()
				if err != nil {
					fmt.Println(err)
					continue
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
		return matches
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
		//temporary response to:  https://github.com/sergi/go-diff/issues/89
		//reference: https://github.com/codeEmitter/gitrob/commit/c735767e86d40a0015756a299e4daeb136c7126b
		defer func() error {
			if err := recover(); err != nil {
				return errors.New(fmt.Sprintf("Panic occurred while retrieving change content: %s", err))
			}
			return nil
		}()

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
