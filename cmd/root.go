package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/viper"

	"github.com/spf13/cobra"
	"github.com/tillson/git-hound/internal/app"
)

var rootCmd = &cobra.Command{
	Use:   "Git Hound",
	Short: "Git Hound is a pattern-matching, batch-catching secret snatcher.",
	Long:  `Git Hound makes it easy to find exposed API keys on GitHub using pattern matching, targetted querying, and a scoring system.`,
	Run: func(cmd *cobra.Command, args []string) {
		color.Green("%s", args)
		ReadConfig()
		client, err := app.LoginToGitHub(app.GitHubCredentials{
			Username: viper.GetString("github_username"),
			Password: viper.GetString("github_password"),
		})
		if err != nil {
			color.Red("[!] Unable to login to GitHub.")
			log.Fatal(err)
		}
		color.Cyan("[*] Logged into GitHub as " + viper.GetString("github_username"))
		_, err = app.Search(args[0], args, client)
		if err != nil {
			color.Red("[!] Unable to collect search results.")
			log.Fatal(err)
		}
		size, err := app.DirSize("/tmp/githound")
		if err != nil && size > 1024*1024*500 {
			app.ClearRepoStorage()
		}
	},
}

// Execute command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func ReadConfig() {
	viper.SetConfigName("config")
	viper.AddConfigPath("$HOME/.githound")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}
}
