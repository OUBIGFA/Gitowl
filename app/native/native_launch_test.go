package main

import (
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

func TestNativeLaunchWaitsForWelcomeButAllowsOAuth(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 8)
	a.routeCoreLaunch(nativeLaunchOptions{openPath: "project"})
	a.routeCoreLaunch(nativeLaunchOptions{cloneURL: "https://example.com/project.git"})
	a.routeCoreLaunch(nativeLaunchOptions{protocolURL: "gitowl://openRepo/https://github.com/example/project"})
	if len(a.coreQueue) != 0 {
		t.Fatal("repository launches can cover the first-run author form")
	}
	a.routeCoreLaunch(nativeLaunchOptions{protocolURL: "x-github-desktop-dev-auth://oauth?code=fixture&state=fixture"})
	if len(a.coreQueue) != 1 {
		t.Fatal("welcome sign-in must still receive its OAuth callback")
	}
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 1, Welcome: true})
	if len(a.coreQueue) != 1 {
		t.Fatal("launches escaped before welcome finished")
	}
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 2})
	if len(a.coreQueue) != 4 {
		t.Fatal("completed welcome did not deliver its deferred launches")
	}
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 3})
	if len(a.coreQueue) != 4 {
		t.Fatal("a later snapshot replayed a launch")
	}
}

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
