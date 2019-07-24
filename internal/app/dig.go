package app

import (
	"log"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/storage/memory"
)

// Dig into the secrets of a repo
func Dig(result RepoSearchResult) (matches []Match) {

	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL: "https://github.com/" + result.Repo,
	})
	if err != nil {
		log.Println("Unable to clone git repo")
		return
	}
	ref, err := repo.Head()
	if err != nil {
		log.Println(err)
		return
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		log.Println(err)
		return
	}

	commitIter, err := repo.Log(&git.LogOptions{From: commit.Hash})
	lastHash, err := commit.Tree()
	if err != nil {
		log.Fatal(err)
	}
	matchMap := make(map[Match]bool)
	err = commitIter.ForEach(
		func(c *object.Commit) error {
			commitTree, err := c.Tree()
			if err != nil {
				return err
			}
			diffMatches := ScanDiff(lastHash, commitTree, result)
			for _, match := range diffMatches {
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
	return matches
}

// ScanDiff finds secrets in the diff between two Git trees.
func ScanDiff(from *object.Tree, to *object.Tree, result RepoSearchResult) (matches []Match) {
	if from == to {
		return
	}
	diff, err := from.Diff(to)
	if err != nil {
		log.Fatal(err)
	}
	for _, change := range diff {
		patch, err := change.Patch()
		if err != nil {
			log.Fatal(err)
		}
		patchStr := patch.String()
		keywordMatches := MatchKeywords(patchStr, result)
		apiMatches := MatchAPIKeys(patchStr, result)
		for _, r := range keywordMatches {
			matches = append(matches, r)
		}
		for _, r := range apiMatches {
			matches = append(matches, r)
		}
	}
	return matches
}
