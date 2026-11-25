package backend

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

type askpassRequest struct{ Token, Prompt string }
type askpassResponse struct {
	Answer string
	Error  string
}
type askpassCall struct {
	prompt string
	answer chan askpassResponse
}

// Askpass uses an authenticated loopback pipe-equivalent owned by one Git
// operation. No credential is written to disk or put in a command argument.
func Askpass(prompt string, output io.Writer) error {
	address, token := os.Getenv("GITOWL_ASKPASS_ADDRESS"), os.Getenv("GITOWL_ASKPASS_TOKEN")
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || len(token) != 64 {
		return errors.New("Git prompt owner is unavailable")
	}
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Minute))
	if err := json.NewEncoder(conn).Encode(askpassRequest{token, prompt}); err != nil {
		return err
	}
	var response askpassResponse
	if err := json.NewDecoder(io.LimitReader(conn, 64<<10)).Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	_, err = fmt.Fprintln(output, response.Answer)
	return err
}

func (n *NativeOwner) runRemote(ctx context.Context, args ...string) error {
	if n.current == nil {
		return errors.New("no repository selected")
	}
	if len(args) == 1 && args[0] == "push" {
		remotes, err := n.current.Run(ctx, nil, "remote")
		if err != nil {
			return err
		}
		if len(remotes) == 0 {
			return n.remoteForm(ctx)
		}
		if n.status.Upstream == "" && n.status.Branch != "" {
			names := strings.Fields(string(remotes))
			if len(names) != 1 {
				return errors.New("configure a tracking branch before pushing to multiple remotes")
			}
			args = []string{"push", "--set-upstream", names[0], n.status.Branch}
		}
	}
	env, err := n.gitCredentialEnvironment(ctx, n.current, n.state.Selected.Repository.Account)
	if err != nil {
		return err
	}
	return n.withGitAskpass(ctx, env, func(ctx context.Context, env []string) error {
		_, err := n.current.RunWithEnvironment(ctx, env, args...)
		return err
	})
}

func (n *NativeOwner) remoteForm(ctx context.Context) error {
	name, remote := "origin", ""
	if n.current == nil {
		return errors.New("no repository selected")
	}
	data, err := n.current.Run(ctx, nil, "remote")
	if err != nil {
		return err
	}
	if names := strings.Fields(string(data)); len(names) > 0 {
		name = names[0]
		value, err := n.current.Run(ctx, nil, "remote", "get-url", name)
		if err != nil {
			return err
		}
		remote = strings.TrimSpace(string(value))
	}
	n.form("NativeRemote", "menu.show-repository-settings", []KernelDialogField{nativeField("name", "repositoryDialog.name", name), nativeField("url", "repositoryDialog.url", remote)})
	return nil
}

func (n *NativeOwner) withGitAskpass(ctx context.Context, environment []string, run func(context.Context, []string) error) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(nonce[:])
	calls := make(chan askpassCall)
	operation, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(10 * time.Minute))
				var request askpassRequest
				if json.NewDecoder(io.LimitReader(conn, 64<<10)).Decode(&request) != nil || subtle.ConstantTimeCompare([]byte(request.Token), []byte(token)) != 1 {
					return
				}
				call := askpassCall{request.Prompt, make(chan askpassResponse, 1)}
				select {
				case calls <- call:
				case <-operation.Done():
					return
				}
				select {
				case answer := <-call.answer:
					_ = json.NewEncoder(conn).Encode(answer)
				case <-operation.Done():
					return
				}
			}()
		}
	}()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	environment = append(append([]string{}, environment...), "GIT_ASKPASS="+executable, "SSH_ASKPASS="+executable, "SSH_ASKPASS_REQUIRE=force", "GIT_TERMINAL_PROMPT=0", "GITOWL_ASKPASS_ADDRESS="+listener.Addr().String(), "GITOWL_ASKPASS_TOKEN="+token)
	finished := make(chan error, 1)
	go func() { finished <- run(operation, environment) }()
	if n.state.Selected != nil {
		n.state.Selected.RemoteInProgress = true
	}
	n.publish()
	defer func() {
		if n.state.Selected != nil {
			n.state.Selected.RemoteInProgress = false
		}
	}()
	for {
		select {
		case err := <-finished:
			return err
		case call := <-calls:
			value, err := n.promptGit(operation, call.prompt)
			response := askpassResponse{Answer: value}
			if err != nil {
				response.Error = "Git authentication was cancelled"
			}
			call.answer <- response
		}
	}
}

func (n *NativeOwner) promptGit(ctx context.Context, prompt string) (string, error) {
	previous, previousList := n.state.CurrentPopup, n.state.Popups
	n.form("NativeGitPrompt", "native.gitPrompt", []KernelDialogField{{ID: "prompt", LabelKey: "native.gitPrompt", Kind: "readonly", ReadOnly: true, Text: prompt}, {ID: "answer", LabelKey: "native.gitAnswer", Kind: "text", Sensitive: !strings.Contains(strings.ToLower(prompt), "username") && !strings.Contains(strings.ToLower(prompt), "yes/no")}})
	id := *n.state.CurrentPopup.ID
	done := make(chan nativeAnswer, 1)
	n.interactionMu.Lock()
	n.modalAnswers[id] = done
	n.interactionMu.Unlock()
	n.publish()
	defer func() {
		n.interactionMu.Lock()
		delete(n.modalAnswers, id)
		n.interactionMu.Unlock()
		n.state.CurrentPopup = previous
		n.state.Popups = previousList
		n.publish()
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case response := <-done:
		if response.err != nil {
			return "", response.err
		}
		value, _ := response.value.(string)
		return value, nil
	}
}

func (n *NativeOwner) answerModal(command nativeCommand) (bool, error) {
	id := command.PopupID
	if command.Name == "popup.reply" {
		id = command.Reply.PopupID
	}
	n.interactionMu.Lock()
	defer n.interactionMu.Unlock()
	done := n.modalAnswers[id]
	if done == nil {
		return false, nil
	}
	answer := nativeAnswer{err: errors.New("dialog cancelled")}
	if command.Name == "popup.reply" && command.Reply.Action == "save" {
		answer = nativeAnswer{value: command.Reply.Values["answer"]}
	}
	select {
	case done <- answer:
		return true, nil
	default:
		return true, errors.New("dialog already answered")
	}
}
