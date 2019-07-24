package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/viper"

	"github.com/spf13/cobra"
	"github.com/tillson/git-hound/internal/app"
)

var rootCmd = &cobra.Command{
	Use:   "Git Hound",
	Short: "Git Hound is a pattern-matching, batch-catching secret snatcher.",
	Long:  `Git Hound makes it easy to find exposed API keys on GitHub using pattern matching, targetted querying, and a scoring system.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(args)
		ReadConfig()
		client, err := app.LoginToGitHub(app.GitHubCredentials{
			Username: viper.GetString("github_username"),
			Password: viper.GetString("github_password"),
		})
		if err != nil {
			log.Println("Unable to login to GitHub.")
			log.Fatal(err)
		}
		fmt.Println("[!] Logged into GitHub as " + viper.GetString("github_username"))
		app.Search("\""+args[0]+"\"", args, client)
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
