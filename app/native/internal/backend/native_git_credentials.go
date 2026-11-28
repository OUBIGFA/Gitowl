package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
	"github.com/egoist/mygo"
)

// CredentialHelper implements Git's documented credential protocol. Tokens
// travel only through its pipe, never command arguments or repository config.
func CredentialHelper(action string, input io.Reader, output io.Writer) error {
	if action != "get" {
		if action == "store" || action == "erase" {
			return nil
		}
		return errors.New("unsupported credential action")
	}
	fields := make(map[string]string)
	scanner := bufio.NewScanner(io.LimitReader(input, 64<<10))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			fields[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	endpoint := os.Getenv("GITOWL_GIT_ACCOUNT_ENDPOINT")
	login := os.Getenv("GITOWL_GIT_ACCOUNT_LOGIN")
	namespace := os.Getenv("GITOWL_GIT_CREDENTIAL_NAMESPACE")
	if endpoint == "" || login == "" {
		return nil
	}
	if namespace != "io.github.oubigfa.gitowl" && !strings.HasPrefix(namespace, "io.github.oubigfa.gitowl.test.") {
		return errors.New("invalid credential namespace")
	}
	html, err := htmlEndpoint(endpoint)
	if err != nil {
		return err
	}
	u, err := url.Parse(html)
	if err != nil {
		return err
	}
	if fields["protocol"] != "https" || !strings.EqualFold(fields["host"], u.Host) {
		return nil
	}
	value, err := mygo.Secrets.Service(credentialService(namespace, endpoint)).Get(login)
	if err != nil {
		fmt.Fprintln(output, "quit=1")
		return errors.New("assigned account credential is unavailable")
	}
	token := string(value)
	if encoded, ok := strings.CutPrefix(token, "github-desktop-oauth:"); ok {
		var record struct {
			Version    int               `json:"version"`
			Credential *nativeCredential `json:"credential"`
		}
		if json.Unmarshal([]byte(encoded), &record) != nil || record.Version != 1 || record.Credential == nil {
			fmt.Fprintln(output, "quit=1")
			return errors.New("assigned account requires sign-in")
		}
		token = record.Credential.AccessToken
		if record.Credential.ExpiresAt > 0 && time.Now().UnixMilli() >= record.Credential.ExpiresAt {
			fmt.Fprintln(output, "quit=1")
			return errors.New("assigned account requires renewal")
		}
	}
	if token == "" || strings.ContainsAny(token+login, "\r\n\x00") {
		fmt.Fprintln(output, "quit=1")
		return errors.New("invalid assigned account credential")
	}
	_, err = fmt.Fprintf(output, "username=%s\npassword=%s\n\n", login, token)
	return err
}

func (n *NativeOwner) gitCredentialEnvironment(ctx context.Context, repository *gitcli.Repository, identity *KernelAccountIdentity) ([]string, error) {
	var environment []string
	if repository != nil {
		environment = append(environment, repository.Environment...)
	}
	if identity == nil {
		return environment, nil
	}
	var account nativeAccount
	for _, candidate := range n.accounts {
		if strings.EqualFold(candidate.Endpoint, identity.Endpoint) && strings.EqualFold(candidate.Login, identity.Login) {
			account = candidate
			break
		}
	}
	if account.Login == "" {
		return nil, errors.New("assigned account requires sign-in")
	}
	if _, err := n.accountCredential(ctx, account); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Git invokes ! helpers through its sh; quote the trusted executable path.
	quoted := "'" + strings.ReplaceAll(strings.ReplaceAll(executable, `\`, "/"), "'", `'\''`) + "'"
	var helpers []byte
	var readErr error
	if repository == nil {
		helpers, readErr = n.git.Global(ctx, "config", "--get-all", "credential.helper")
	} else {
		helpers, readErr = repository.Run(ctx, nil, "config", "--get-all", "credential.helper")
	}
	if readErr != nil {
		var command *gitcli.CommandError
		if !errors.As(readErr, &command) || command.ExitCode != 1 {
			return nil, readErr
		}
	}
	values := []string{"", "!" + quoted + " --credential-helper"}
	for _, helper := range strings.Split(strings.TrimSuffix(string(helpers), "\n"), "\n") {
		if helper != "" {
			values = append(values, helper)
		}
	}
	base, _ := strconv.Atoi(os.Getenv("GIT_CONFIG_COUNT"))
	if base < 0 || base > 1024 {
		return nil, errors.New("invalid inherited Git command configuration")
	}
	environment = append(environment, "GITOWL_GIT_ACCOUNT_ENDPOINT="+account.Endpoint, "GITOWL_GIT_ACCOUNT_LOGIN="+account.Login, "GITOWL_GIT_CREDENTIAL_NAMESPACE="+n.credentialNamespace, fmt.Sprintf("GIT_CONFIG_COUNT=%d", base+len(values)))
	for i, value := range values {
		environment = append(environment, fmt.Sprintf("GIT_CONFIG_KEY_%d=credential.helper", base+i), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", base+i, value))
	}
	return environment, nil
}
