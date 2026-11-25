package backend

import (
	"context"
	"errors"
	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
	"strings"
)

func (n *NativeOwner) welcome(ctx context.Context, step string) error {
	if step != "Start" && step != "ConfigureGit" && step != "SignInToDotComWithBrowser" && step != "SignInToEnterprise" {
		return errors.New("unknown welcome step")
	}
	n.state.Welcome = true
	n.state.Onboarding = &KernelOnboarding{Step: step}
	if step == "ConfigureGit" {
		for _, item := range []struct {
			key  string
			dest **string
		}{{"user.name", &n.state.Onboarding.GlobalName}, {"user.email", &n.state.Onboarding.GlobalEmail}} {
			data, err := n.git.Global(ctx, "config", "--global", "--get", item.key)
			if err != nil {
				var command *gitcli.CommandError
				if !errors.As(err, &command) || command.ExitCode != 1 {
					return err
				}
			}
			*item.dest = textPointer(strings.TrimSpace(string(data)))
		}
	}
	if step == "SignInToDotComWithBrowser" {
		return n.beginSignIn(ctx, "https://api.github.com")
	}
	if step == "SignInToEnterprise" {
		n.state.SignIn = &KernelSignIn{Kind: "EndpointEntry"}
	}
	return nil
}
func (n *NativeOwner) configureAuthor(ctx context.Context, name, email string) error {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" || email == "" || strings.ContainsAny(name+email, "\r\n\x00") {
		return errors.New("enter a Git author name and email address")
	}
	if _, err := n.git.Global(ctx, "config", "--global", "user.name", name); err != nil {
		return err
	}
	if _, err := n.git.Global(ctx, "config", "--global", "user.email", email); err != nil {
		return err
	}
	if err := n.store.Set(ctx, "has-shown-welcome-flow", "1"); err != nil {
		return err
	}
	n.state.Welcome = false
	n.state.Onboarding = nil
	return nil
}
