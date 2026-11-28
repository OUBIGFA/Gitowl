package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

type nativeAccount struct {
	Login     string        `json:"login"`
	Endpoint  string        `json:"endpoint"`
	Name      string        `json:"name"`
	ID        int64         `json:"id"`
	AvatarURL string        `json:"avatarURL"`
	Emails    []nativeEmail `json:"emails"`
}
type nativeEmail struct {
	Email      string `json:"email"`
	Verified   bool   `json:"verified"`
	Primary    bool   `json:"primary"`
	Visibility string `json:"visibility,omitempty"`
}
type nativeCredential struct {
	AccessToken           string `json:"accessToken"`
	RefreshToken          string `json:"refreshToken,omitempty"`
	ExpiresAt             int64  `json:"expiresAt,omitempty"`
	RefreshTokenExpiresAt int64  `json:"refreshTokenExpiresAt,omitempty"`
}

func (n *NativeOwner) loadAccounts(ctx context.Context) error {
	raw, err := n.store.Get(ctx, "users")
	if err != nil {
		return err
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &n.accounts); err != nil {
			return errors.New("saved account metadata is invalid; original records were preserved")
		}
	}
	n.state.Accounts = []KernelAccount{}
	oldSelections := map[string]map[string]string{}
	if raw, err := n.store.Get(ctx, "selected-copilot-models-by-account"); err != nil {
		return err
	} else if raw != "" {
		if err := json.Unmarshal([]byte(raw), &oldSelections); err != nil {
			return errors.New("saved Copilot selections are invalid; original data preserved")
		}
	}
	for _, account := range n.accounts {
		key := account.Endpoint + "\n" + account.Login
		raw, err := n.store.Get(ctx, "native-copilot-catalog:"+key)
		if err != nil {
			return err
		}
		if raw != "" {
			var models []ProviderModel
			if err := json.Unmarshal([]byte(raw), &models); err != nil {
				return err
			}
			n.copilotModels[key] = models
		} else if previous := oldSelections[fmt.Sprintf("%d:%s", account.ID, account.Endpoint)]["commit-message-generation"]; previous != "" && !strings.HasPrefix(previous, "byok:") {
			model := strings.TrimPrefix(previous, "copilot:")
			// Preserve the explicit old choice, but require a real catalog refresh
			// before treating it as available. Never silently choose another model.
			models := []ProviderModel{{ID: model, Name: model, Enabled: true, Available: false}}
			data, _ := json.Marshal(models)
			if err := n.store.Set(ctx, "native-copilot-catalog:"+key, string(data)); err != nil {
				return err
			}
			if err := n.store.Set(ctx, "native-copilot-model:"+account.Endpoint+"/"+account.Login, model); err != nil {
				return err
			}
			n.copilotModels[key] = models
		}
		n.state.Accounts = append(n.state.Accounts, KernelAccount{Endpoint: account.Endpoint, Login: account.Login, Name: account.Name, CopilotEnabled: len(n.copilotModels[key]) > 0})
	}
	return nil
}
func credentialService(namespace, endpoint string) string {
	return namespace + " :: GitHub - " + endpoint
}

// Keep the exact legacy keytar service/login. MyGo's documented Secrets.Service
// interface reads those records without exporting secrets to an application file.
func (n *NativeOwner) accountCredential(ctx context.Context, account nativeAccount) (nativeCredential, error) {
	if err := ctx.Err(); err != nil {
		return nativeCredential{}, err
	}
	data, err := mygo.Secrets.Service(credentialService(n.credentialNamespace, account.Endpoint)).Get(account.Login)
	if err != nil {
		return nativeCredential{}, err
	}
	var token nativeCredential
	if value, ok := strings.CutPrefix(string(data), "github-desktop-oauth:"); ok {
		var record struct {
			Version    int               `json:"version"`
			Credential *nativeCredential `json:"credential"`
		}
		if json.Unmarshal([]byte(value), &record) != nil || record.Version != 1 || record.Credential == nil {
			return token, errors.New("account requires sign-in")
		}
		token = *record.Credential
	} else {
		token.AccessToken = string(data)
	}
	if token.AccessToken == "" || strings.ContainsAny(token.AccessToken, " \r\n\t") {
		return nativeCredential{}, errors.New("account credential is invalid")
	}
	if token.ExpiresAt > 0 && time.Now().Add(2*time.Minute).UnixMilli() >= token.ExpiresAt {
		if token.RefreshToken == "" || (token.RefreshTokenExpiresAt > 0 && time.Now().UnixMilli() >= token.RefreshTokenExpiresAt) {
			return nativeCredential{}, errors.New("account requires sign-in")
		}
		updated, err := n.oauthToken(ctx, account.Endpoint, map[string]string{"grant_type": "refresh_token", "refresh_token": token.RefreshToken})
		if err != nil {
			return nativeCredential{}, err
		}
		if updated.RefreshToken == "" {
			return nativeCredential{}, errors.New("OAuth renewal did not return a rotating token pair")
		}
		if err := n.saveCredential(account, updated); err != nil {
			return nativeCredential{}, err
		}
		token = updated
	}
	return token, nil
}
func (n *NativeOwner) assignedAccount() (nativeAccount, error) {
	if n.state.Selected == nil {
		return nativeAccount{}, errors.New("no selected repository")
	}
	repository := n.state.Selected.Repository
	if repository.Account == nil {
		return nativeAccount{}, errors.New("assign a signed-in account to this repository")
	}
	for _, account := range n.accounts {
		if strings.EqualFold(account.Login, repository.Account.Login) && strings.EqualFold(account.Endpoint, repository.Account.Endpoint) {
			return account, nil
		}
	}
	return nativeAccount{}, errors.New("assigned account requires sign-in")
}
func providerBinding(p ProviderSettings) (string, error) {
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", errors.New("invalid provider address")
	}
	return "gitowl-provider-v1\n" + p.Format + "\n" + p.AuthKind + "\n" + u.Scheme + "://" + u.Host + "\n", nil
}
func (n *NativeOwner) providerSecret(p Provider) (string, error) {
	if p.AuthKind == "none" {
		return "", nil
	}
	binding, err := providerBinding(p.ProviderSettings)
	if err != nil {
		return "", err
	}
	value, err := mygo.Secrets.Service(n.credentialNamespace + " :: Gitowl").Get("commit-generation-provider:" + p.ID)
	if err != nil {
		return "", errors.New("provider credential is unavailable")
	}
	secret, ok := strings.CutPrefix(string(value), binding)
	if !ok || secret == "" {
		return "", errors.New("provider credential does not match this address and protocol")
	}
	return secret, nil
}
