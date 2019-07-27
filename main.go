package main

import (
	_ "net/http/pprof"

	"github.com/tillson/git-hound/cmd"
)

func main() {
	cmd.Execute()
}
