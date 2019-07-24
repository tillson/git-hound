// Dig into Git repos
package app

import (
	"log"

	git "gopkg.in/src-d/go-git.v4"
)

// Dig into the secrets of a repo
func Dig(cloneURL string) {
	repo, err := git.PlainClone("/tmp/test/", false, &git.CloneOptions{
		URL: cloneURL,
	})
	if err != nil {
		log.Println("Unable to clone git repo")
		log.Println(err)
	}
	log.Println(repo)
}
