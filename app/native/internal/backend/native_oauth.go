package backend

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/egoist/mygo"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type nativeOAuthConfig struct {
	ClientID       string `json:"clientID"`
	ClientSecret   string `json:"clientSecret"`
	CallbackScheme string `json:"oauthCallbackScheme"`
}
type nativeOAuthPending struct {
	state, endpoint string
	expires         time.Time
}

func htmlEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return "", errors.New("account endpoint must use HTTPS")
	}
	if u.Host == "api.github.com" {
		return "https://github.com", nil
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/v3")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}
func (n *NativeOwner) readOAuthConfig() error {
	data, err := os.ReadFile(filepath.Join(n.resources, "native-launch.json"))
	if err != nil {
		return err
	}
	if json.Unmarshal(data, &n.oauthConfig) != nil || n.oauthConfig.ClientID == "" {
		return errors.New("OAuth configuration is missing")
	}
	return nil
}
func (n *NativeOwner) beginSignIn(ctx context.Context, endpoint string) error {
	if endpoint == "" {
		endpoint = "https://api.github.com"
	}
	html, err := htmlEndpoint(endpoint)
	if err != nil {
		return err
	}
	if err := n.readOAuthConfig(); err != nil {
		return err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	n.oauthPending = &nativeOAuthPending{hex.EncodeToString(nonce[:]), endpoint, time.Now().Add(10 * time.Minute)}
	query := url.Values{"client_id": {n.oauthConfig.ClientID}, "scope": {"repo user workflow"}, "state": {n.oauthPending.state}}
	n.state.SignIn = &KernelSignIn{Kind: "Authentication", Loading: true, Endpoint: &endpoint}
	n.interactionID++
	id := n.interactionID
	n.state.CurrentPopup = &KernelPopupPresentation{ID: &id, Type: "SignIn", Supported: true}
	n.state.Popups = []KernelPopup{{ID: &id, Type: "SignIn"}}
	n.publish()
	if err := n.openExternal(ctx, html+"/login/oauth/authorize?"+query.Encode()); err != nil {
		n.state.SignIn.Loading = false
		message := err.Error()
		n.state.SignIn.Error = &message
		return err
	}
	return nil
}
func (n *NativeOwner) oauthToken(ctx context.Context, endpoint string, params map[string]string) (nativeCredential, error) {
	if n.oauthConfig.ClientID == "" {
		if err := n.readOAuthConfig(); err != nil {
			return nativeCredential{}, err
		}
	}
	html, err := htmlEndpoint(endpoint)
	if err != nil {
		return nativeCredential{}, err
	}
	params["client_id"] = n.oauthConfig.ClientID
	params["client_secret"] = n.oauthConfig.ClientSecret
	var result struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
		Error            string `json:"error"`
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := nativeJSON(ctx, n.httpClient, http.MethodPost, html+"/login/oauth/access_token", params, nil, &result); err != nil {
		return nativeCredential{}, err
	}
	if result.Error != "" || result.AccessToken == "" || strings.ContainsAny(result.AccessToken, " \r\n\t") {
		return nativeCredential{}, errors.New("OAuth authorization was rejected")
	}
	credential := nativeCredential{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken}
	now := time.Now().UnixMilli()
	if result.ExpiresIn > 0 {
		credential.ExpiresAt = now + result.ExpiresIn*1000
	}
	if result.RefreshExpiresIn > 0 {
		credential.RefreshTokenExpiresAt = now + result.RefreshExpiresIn*1000
	}
	return credential, nil
}
func (n *NativeOwner) saveCredential(account nativeAccount, credential nativeCredential) error {
	value := credential.AccessToken
	if credential.RefreshToken != "" {
		data, err := json.Marshal(map[string]any{"version": 1, "credential": credential})
		if err != nil {
			return err
		}
		value = "github-desktop-oauth:" + string(data)
	}
	return mygo.Secrets.Service(credentialService(n.credentialNamespace, account.Endpoint)).Set(account.Login, []byte(value))
}
func (n *NativeOwner) finishSignIn(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	pending := n.oauthPending
	if pending == nil || time.Now().After(pending.expires) || u.Scheme != n.oauthConfig.CallbackScheme || u.Host != "oauth" || subtle.ConstantTimeCompare([]byte(u.Query().Get("state")), []byte(pending.state)) != 1 {
		return errors.New("OAuth callback does not match the pending sign-in")
	}
	n.oauthPending = nil
	code := u.Query().Get("code")
	if code == "" {
		return errors.New("OAuth callback has no authorization code")
	}
	credential, err := n.oauthToken(ctx, pending.endpoint, map[string]string{"code": code})
	if err != nil {
		return err
	}
	var account nativeAccount
	headers := http.Header{"Authorization": []string{"Bearer " + credential.AccessToken}}
	if err := nativeJSON(ctx, n.httpClient, http.MethodGet, strings.TrimSuffix(pending.endpoint, "/")+"/user", nil, headers, &account); err != nil {
		return err
	}
	if account.Login == "" || account.ID <= 0 {
		return errors.New("account identity is missing")
	}
	account.Endpoint = pending.endpoint
	if err := nativeJSON(ctx, n.httpClient, http.MethodGet, strings.TrimSuffix(pending.endpoint, "/")+"/user/emails", nil, headers, &account.Emails); err != nil {
		return err
	}
	if err := n.saveCredential(account, credential); err != nil {
		return err
	}
	found := false
	for i, old := range n.accounts {
		if strings.EqualFold(old.Endpoint, account.Endpoint) && strings.EqualFold(old.Login, account.Login) {
			n.accounts[i] = account
			found = true
		}
	}
	if !found {
		n.accounts = append(n.accounts, account)
	}
	data, err := json.Marshal(n.accounts)
	if err != nil {
		return err
	}
	if err := n.store.Set(ctx, "users", string(data)); err != nil {
		return err
	}
	if err := n.loadAccounts(ctx); err != nil {
		return err
	}
	n.state.SignIn = nil
	n.state.CurrentPopup = nil
	n.state.Popups = []KernelPopup{}
	return nil
}
