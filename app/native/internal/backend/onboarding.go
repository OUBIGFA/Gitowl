package backend

import "context"

// These values describe the original Welcome/SignIn presentation only. OAuth
// state, callbacks, token pairs and Account objects remain in the Desktop realm.
type KernelSignIn struct {
	Kind                         string  `json:"kind"`
	Loading                      bool    `json:"loading"`
	Error                        *string `json:"error"`
	Endpoint                     *string `json:"endpoint"`
	ExpectedLogin                *string `json:"expectedLogin"`
	UnrecognizedEnterpriseServer bool    `json:"unrecognizedEnterpriseServer"`
}

type KernelOnboardingAccount struct {
	Name           string   `json:"name"`
	PreferredEmail string   `json:"preferredEmail"`
	Emails         []string `json:"emails"`
	Enterprise     bool     `json:"enterprise"`
}

type KernelOnboarding struct {
	Step         string                   `json:"step"`
	Loading      bool                     `json:"loading"`
	GlobalName   *string                  `json:"globalName"`
	GlobalEmail  *string                  `json:"globalEmail"`
	LockFilePath *string                  `json:"lockFilePath"`
	Account      *KernelOnboardingAccount `json:"account"`
}

func (k *Kernel) AdvanceWelcome(ctx context.Context, step string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
		Step string `json:"step"`
	}{"welcome.advance", step})
}

func (k *Kernel) ConfigureWelcomeAuthor(ctx context.Context, name, email string, useAccount bool) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string `json:"name"`
		NameValue  string `json:"nameValue"`
		Email      string `json:"email"`
		UseAccount bool   `json:"useAccount"`
	}{"welcome.configureGit", name, email, useAccount})
}

func (k *Kernel) UnlockWelcomeGitConfig(ctx context.Context) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
	}{"welcome.unlockGit"})
}

func (k *Kernel) SetSignInEndpoint(ctx context.Context, endpoint string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name     string `json:"name"`
		Endpoint string `json:"endpoint"`
	}{"signIn.endpoint", endpoint})
}

func (k *Kernel) AuthenticateUsingBrowser(ctx context.Context) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
	}{"signIn.browser"})
}

func (k *Kernel) ResetSignIn(ctx context.Context) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
	}{"signIn.reset"})
}
