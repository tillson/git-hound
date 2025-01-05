package tests

import (
	"testing"

	"github.com/GRbit/go-pcre"
	"github.com/tillson/git-hound/internal/app"
)

func TestMatchKeywords(t *testing.T) {
	matches := app.MatchKeywords("odt_KTJlDq2AGGGlqG4riKdT7p980AW8RlU5")
	if len(matches) < 1 {
		t.Errorf("Keyword was not found in string.")
	}
}

func TestPCRERegex(t *testing.T) {
	regex := `odt_[A-Za-z0-9]{32}`
	str := "odt_KTJlDq2AGGGlqG4riKdT7p980AW8RlU5odt_KTJlDq2AGGGlqG4riKdT7p980AW8RlU5odt_KTJlDq2AGGGlqG4riKdT7p980AW8RlU5"

	re, err := pcre.Compile(regex, 0)
	if err != nil {
		t.Fatalf("Failed to compile regex: %s", err)
	}

	matched := re.FindAllIndex([]byte(str), 0)
	for _, match := range matched {
		t.Logf("Matched: %s", str[match[0]:match[1]])
	}
	if len(matched) != 3 {
		t.Errorf("Regex did not match string.")
	}
}

func TestBase64EncodedKeyword(t *testing.T) {
	matches := app.MatchKeywords("This is a test. <ZGJfcGFzc3dvcmQ9dGhpc2lzYWJhZHBhc3N3b3JkCg==> This is a test")
	if len(matches) < 1 {
		t.Errorf("Keyword was not found in base64 encoded string")
	}
}
