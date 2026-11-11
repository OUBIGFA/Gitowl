package main

import "testing"

func TestSecondInstancePreservesSinglePositionalArgument(t *testing.T) {
	options, err := parseSecondInstanceArguments([]string{"gitowl-auth://oauth?code=one&state=two"}, `C:\work`)
	if err != nil {
		t.Fatal(err)
	}
	if options.protocolURL != "gitowl-auth://oauth?code=one&state=two" {
		t.Fatalf("first forwarded argument was lost: %+v", options)
	}
}

func TestSecondInstancePreservesRepositoryAndCloneArguments(t *testing.T) {
	open, err := parseSecondInstanceArguments([]string{`C:\work\repo`}, `C:\work`)
	if err != nil || open.openPath != `C:\work\repo` {
		t.Fatalf("repository launch: %+v, %v", open, err)
	}
	clone, err := parseSecondInstanceArguments([]string{"--cli-clone", "https://github.com/example/project", "--cli-branch", "feature"}, `C:\work`)
	if err != nil || clone.cloneURL != "https://github.com/example/project" || clone.branch != "feature" {
		t.Fatalf("clone launch: %+v, %v", clone, err)
	}
}

func TestOriginalAndGitowlProtocolsAreAcceptedByNativeHost(t *testing.T) {
	for _, test := range []struct {
		url string
		ok  bool
	}{
		{"gitowl://openRepo/https://github.com/example/project", true},
		{"gitowl-auth://oauth?code=one&state=two", true},
		{"x-github-desktop-auth://oauth?code=one&state=two", true},
		{"x-github-desktop-dev-auth://oauth?code=a%2Bb&state=x%2Fy", true},
		{"X-GitHub-Desktop-Dev-Auth://oauth?code=one&state=two", true},
		{"x-github-client://openRepo/https://github.com/example/project", true},
		{"https://github.com/example/project", false},
	} {
		if got := supportedApplicationURL(test.url); got != test.ok {
			t.Errorf("supportedApplicationURL(%q) = %t, want %t", test.url, got, test.ok)
		}
	}
}
