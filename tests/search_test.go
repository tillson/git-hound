package tests

import (
	"testing"

	"github.com/tillson/git-hound/internal/app"
)

func TestMatchKeywords(t *testing.T) {
	matches := app.MatchKeywords("config.yml: db_password=thisisabadpassword")
	if len(matches) < 1 {
		t.Errorf("Keyword was not found in string.")
	}
}

func TestBase64EncodedKeyword(t *testing.T) {
	matches := app.MatchKeywords("This is a test. <ZGJfcGFzc3dvcmQ9dGhpc2lzYWJhZHBhc3N3b3JkCg==> This is a test")
	if len(matches) < 1 {
		t.Errorf("Keyword was not found in base64 encoded string")
	}
}
