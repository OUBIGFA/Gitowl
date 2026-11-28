package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// Child invocations use the production helper functions, so Git really spawns
// and communicates with an executable instead of a mocked credential callback.
func TestMain(m *testing.M) {
	if os.Getenv("GITOWL_HELPER_TEST") == "1" {
		if len(os.Args) == 3 && os.Args[1] == "--credential-helper" {
			if err := CredentialHelper(os.Args[2], os.Stdin, os.Stdout); err != nil {
				os.Exit(1)
			}
			return
		}
		if len(os.Args) == 2 && os.Getenv("GITOWL_ASKPASS_ADDRESS") != "" {
			if err := Askpass(os.Args[1], os.Stdout); err != nil {
				os.Exit(1)
			}
			return
		}
	}
	os.Exit(m.Run())
}

func TestNativeCredentialHelperIsolatesHostsAndExpiredTokens(t *testing.T) {
	ctx := context.Background()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_ENV", "1")
	k, err := NewNativeKernel(ctx, git, filepath.Join(t.TempDir(), "profile"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	n := k.native
	account := nativeAccount{Login: "fixture", Endpoint: "https://api.github.com"}
	service := mygo.Secrets.Service(credentialService(n.credentialNamespace, account.Endpoint))
	t.Cleanup(func() { service.Delete(account.Login) })
	if err := n.saveCredential(account, nativeCredential{AccessToken: "fixture-token"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITOWL_HELPER_TEST", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "empty.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	n.accounts = []nativeAccount{account}
	env, err := n.gitCredentialEnvironment(ctx, nil, &KernelAccountIdentity{Endpoint: account.Endpoint, Login: account.Login})
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, git, "credential", "fill")
	child.Env = append(os.Environ(), env...)
	child.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	result, err := child.Output()
	if err != nil || !strings.Contains(string(result), "password=fixture-token") {
		t.Fatal("Git could not use the native credential helper")
	}
	t.Setenv("GITOWL_GIT_ACCOUNT_ENDPOINT", account.Endpoint)
	t.Setenv("GITOWL_GIT_ACCOUNT_LOGIN", account.Login)
	t.Setenv("GITOWL_GIT_CREDENTIAL_NAMESPACE", n.credentialNamespace)
	for _, tc := range []struct {
		host, protocol string
		want           bool
	}{{"github.com", "https", true}, {"elsewhere.invalid", "https", false}, {"github.com", "http", false}, {"github.com.evil.invalid", "https", false}} {
		var out bytes.Buffer
		err := CredentialHelper("get", strings.NewReader("protocol="+tc.protocol+"\nhost="+tc.host+"\n\n"), &out)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "fixture-token") != tc.want {
			t.Fatal("credential was routed to the wrong host")
		}
	}
	if err := n.saveCredential(account, nativeCredential{AccessToken: "expired", RefreshToken: "fixture-refresh", ExpiresAt: time.Now().Add(-time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := CredentialHelper("get", strings.NewReader("protocol=https\nhost=github.com\n\n"), &out); err == nil || !strings.Contains(out.String(), "quit=1") || strings.Contains(out.String(), "expired") {
		t.Fatal("expired account fell through to other credentials")
	}
}

func TestNativeOAuthNonceAndRenewal(t *testing.T) {
	ctx := context.Background()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_ENV", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "empty.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	k, err := NewNativeKernel(ctx, git, filepath.Join(t.TempDir(), "profile"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	n := k.native
	n.oauthConfig = nativeOAuthConfig{ClientID: "fixture", ClientSecret: "fixture-secret", CallbackScheme: "x-github-desktop-auth"}
	n.resources = t.TempDir()
	config, err := json.Marshal(n.oauthConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(n.resources, "native-launch.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	var state string
	n.onEvent = func(event KernelEvent) {
		if event.External == nil {
			return
		}
		address, err := url.Parse(event.External.URL)
		if err != nil {
			t.Fatal(err)
		}
		state = address.Query().Get("state")
		if address.Host != "github.com" || address.Path != "/login/oauth/authorize" || state == "" {
			t.Fatal("browser authentication did not receive a GitHub authorization URL and nonce")
		}
		if err := k.ReplyExternal(ctx, event.External.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	n.httpClient = &http.Client{Transport: componentTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		body := `{}`
		switch r.URL.Path {
		case "/login/oauth/access_token":
			var params map[string]string
			if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
				t.Error(err)
			}
			if params["client_id"] != "fixture" {
				t.Error("wrong OAuth app")
			}
			body = `{"access_token":"new-fixture-token","refresh_token":"new-refresh","expires_in":3600,"refresh_token_expires_in":7200}`
		case "/user":
			if r.Header.Get("Authorization") != "Bearer new-fixture-token" {
				t.Error("wrong token")
			}
			body = `{"id":42,"login":"fixture","name":"Fixture"}`
		case "/user/emails":
			body = `[{"email":"fixture@example.invalid","verified":true,"primary":true}]`
		default:
			t.Errorf("unexpected OAuth endpoint %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if _, err := k.AdvanceWelcome(ctx, "SignInToDotComWithBrowser"); err != nil {
		t.Fatal(err)
	}
	if err := n.finishSignIn(ctx, "x-github-desktop-auth://oauth?state=wrong&code=fixture"); err == nil || requests != 0 {
		t.Fatal("unmatched callback made a request")
	}
	t.Cleanup(func() {
		mygo.Secrets.Service(credentialService(n.credentialNamespace, "https://api.github.com")).Delete("fixture")
	})
	callback := "x-github-desktop-auth://oauth?state=" + state + "&code=fixture"
	result, err := k.DispatchApplicationURL(ctx, callback)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.accounts) != 1 || len(n.accounts[0].Emails) != 1 || requests != 3 {
		t.Fatal("account metadata incomplete")
	}
	// The public callback must finish the visible login step, not just save a
	// token. Otherwise Welcome renders an empty, permanently waiting page.
	welcome := result.Snapshot.Onboarding
	if welcome == nil || welcome.Step != "ConfigureGit" || welcome.Account == nil || welcome.Account.Name != "Fixture" || welcome.Account.PreferredEmail != "fixture@example.invalid" {
		t.Fatalf("successful OAuth did not advance Welcome with the account identity: %+v", welcome)
	}
	if result.Snapshot.SignIn != nil || result.Snapshot.CurrentPopup != nil || !result.Snapshot.Welcome {
		t.Fatal("successful OAuth left authentication open or skipped author confirmation")
	}
	if err := n.finishSignIn(ctx, callback); err == nil || requests != 3 {
		t.Fatal("OAuth callback replay accepted")
	}
	if err := n.saveCredential(n.accounts[0], nativeCredential{AccessToken: "old", RefreshToken: "old-refresh", ExpiresAt: 1, RefreshTokenExpiresAt: time.Now().Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	token, err := n.accountCredential(ctx, n.accounts[0])
	if err != nil || token.AccessToken != "new-fixture-token" || requests != 4 {
		t.Fatalf("renewal failed %v", err)
	}
	// Leaving the first-run login page still protects the user's navigation
	// when a valid browser callback arrives late, as in the upstream flow.
	if _, err := k.AdvanceWelcome(ctx, "SignInToDotComWithBrowser"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.AdvanceWelcome(ctx, "Start"); err != nil {
		t.Fatal(err)
	}
	result, err = k.DispatchApplicationURL(ctx, "x-github-desktop-auth://oauth?state="+state+"&code=fixture")
	if err != nil || result.Snapshot.Onboarding.Step != "Start" {
		t.Fatal("a late OAuth callback reopened the canceled setup step", err)
	}
	n.state.Welcome, n.state.Onboarding = false, nil
	if _, err := k.SignIn(ctx, nil); err != nil {
		t.Fatal(err)
	}
	result, err = k.DispatchApplicationURL(ctx, "x-github-desktop-auth://oauth?state="+state+"&code=fixture")
	if err != nil || result.Snapshot.Welcome || result.Snapshot.Onboarding != nil || result.Snapshot.CurrentPopup != nil {
		t.Fatal("settings sign-in unexpectedly started first-run setup", err)
	}
}

func TestNativeWelcomeUsesSavedAccountForAuthorConfirmation(t *testing.T) {
	ctx := context.Background()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_ENV", "1")
	configPath := filepath.Join(t.TempDir(), "empty.gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", configPath)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	k, err := NewNativeKernel(ctx, git, filepath.Join(t.TempDir(), "profile"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	// This is the installed failure's persisted state: an account was saved
	// successfully, but first-run author confirmation has not completed.
	const account = `[{"login":"fixture","id":42,"endpoint":"https://api.github.com","emails":[{"email":"private@example.invalid","primary":true,"visibility":"private"},{"email":"42+fixture@users.noreply.github.com","primary":false}]}]`
	if err := k.native.store.Set(ctx, "users", account); err != nil {
		t.Fatal(err)
	}
	if err := k.native.loadAccounts(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := k.AdvanceWelcome(ctx, "ConfigureGit")
	if err != nil {
		t.Fatal(err)
	}
	identity := result.Snapshot.Onboarding.Account
	if identity == nil || identity.Name != "fixture" || identity.PreferredEmail != "42+fixture@users.noreply.github.com" || len(identity.Emails) != 2 {
		t.Fatalf("saved account was lost or its private address displaced no-reply: %+v", identity)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) || !result.Snapshot.Welcome {
		t.Fatal("displaying the saved identity wrote Git config or bypassed confirmation")
	}
}

func TestNativeAskpassModalAnswersWithoutOwnerLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan int, 1)
	k, err := NewNativeKernel(ctx, git, filepath.Join(t.TempDir(), "profile"), func(event KernelEvent) {
		if event.Snapshot != nil && event.Snapshot.CurrentPopup != nil && event.Snapshot.CurrentPopup.Type == "NativeGitPrompt" {
			select {
			case seen <- *event.Snapshot.CurrentPopup.ID:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	result := make(chan error, 1)
	go func() {
		k.native.mu.Lock()
		defer k.native.mu.Unlock()
		answer, err := k.native.promptGit(ctx, "Password for fixture host")
		if err == nil && answer != "fixture-answer" {
			err = io.ErrUnexpectedEOF
		}
		result <- err
	}()
	select {
	case id := <-seen:
		if _, err := k.ReplyPopup(ctx, KernelPopupReply{PopupID: id, Action: "save", Values: map[string]any{"answer": "fixture-answer"}}); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("prompt answer waited on owner lock")
	}
}

func TestNativeGitSpawnsAskpassAndCancelsPrompt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITOWL_HELPER_TEST", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "empty.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	events := make(chan int, 2)
	k, err := NewNativeKernel(ctx, git, filepath.Join(t.TempDir(), "profile"), func(event KernelEvent) {
		if event.Snapshot != nil && event.Snapshot.CurrentPopup != nil && event.Snapshot.CurrentPopup.Type == "NativeGitPrompt" {
			select {
			case events <- *event.Snapshot.CurrentPopup.ID:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	for _, cancelPrompt := range []bool{false, true} {
		result := make(chan error, 1)
		go func() {
			k.native.mu.Lock()
			defer k.native.mu.Unlock()
			result <- k.native.withGitAskpass(ctx, nil, func(ctx context.Context, env []string) error {
				child := exec.CommandContext(ctx, git, "-c", "credential.helper=", "credential", "fill")
				child.Env = append(os.Environ(), env...)
				child.Stdin = strings.NewReader("protocol=https\nhost=fixture.invalid\nusername=fixture\n\n")
				data, err := child.Output()
				if err == nil && !strings.Contains(string(data), "password=fixture-answer") {
					return io.ErrUnexpectedEOF
				}
				return err
			})
		}()
		select {
		case id := <-events:
			reply := KernelPopupReply{PopupID: id, Action: "save", Values: map[string]any{"answer": "fixture-answer"}}
			if cancelPrompt {
				reply.Action = "cancel"
			}
			if _, err := k.ReplyPopup(ctx, reply); err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		select {
		case err := <-result:
			if (err != nil) != cancelPrompt {
				t.Fatalf("cancel=%v result=%v", cancelPrompt, err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
