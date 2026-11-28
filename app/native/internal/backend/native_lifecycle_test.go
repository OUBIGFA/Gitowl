package backend

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestNativeCloseCancelsActiveRequest(t *testing.T) {
	k, _ := nativeRepositoryFixture(t)
	n := k.native
	n.oauthConfig = nativeOAuthConfig{ClientID: "fixture", CallbackScheme: "x-github-desktop-auth"}
	n.oauthPending = &nativeOAuthPending{state: "fixture", endpoint: "https://api.github.com", expires: time.Now().Add(time.Minute)}
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	n.httpClient = &http.Client{Transport: componentTransport(func(r *http.Request) (*http.Response, error) {
		close(entered)
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-release:
			return nil, errors.New("released by test cleanup")
		}
	})}
	result := make(chan error, 1)
	go func() {
		_, err := k.DispatchApplicationURL(context.Background(), "x-github-desktop-auth://oauth?state=fixture&code=fixture")
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("OAuth request did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- k.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close waited for a network request after cancelling the owner")
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("request did not observe shutdown: %v", err)
	}
}

func TestNativeCancelledCredentialRenewalDoesNotStartHTTP(t *testing.T) {
	k, _ := nativeRepositoryFixture(t)
	n := k.native
	account := nativeAccount{Login: "fixture", Endpoint: "https://api.github.com"}
	n.accounts = []nativeAccount{account}
	n.oauthConfig = nativeOAuthConfig{ClientID: "fixture"}
	if err := n.saveCredential(account, nativeCredential{AccessToken: "expired", RefreshToken: "fixture-refresh", ExpiresAt: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mygo.Secrets.Service(credentialService(n.credentialNamespace, account.Endpoint)).Delete(account.Login)
	})
	requests := 0
	n.httpClient = &http.Client{Transport: componentTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("cancelled operation must not renew credentials")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := n.gitCredentialEnvironment(ctx, n.current, &KernelAccountIdentity{Endpoint: account.Endpoint, Login: account.Login})
	if !errors.Is(err, context.Canceled) || requests != 0 {
		t.Fatalf("credential renewal ignored caller cancellation: requests=%d err=%v", requests, err)
	}
}
