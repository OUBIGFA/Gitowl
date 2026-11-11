package backend

import (
	"context"
	"encoding/json"
	"testing"
)

func TestLaunchCommandsPreserveOriginalDispatcherActions(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*Kernel) <-chan struct{ result KernelResult; err error }
		want string
	}{
		{
			name: "open repository",
				call: func(k *Kernel) <-chan struct{ result KernelResult; err error } {
					return kernelCall(func() (KernelResult, error) { return k.OpenFromCLI(context.Background(), `C:\work\repo`) })
				},
				want: `{"name":"launch.cli","action":{"kind":"open-repository","path":"C:\\work\\repo"}}`,
		},
		{
			name: "clone branch",
			call: func(k *Kernel) <-chan struct{ result KernelResult; err error } {
				return kernelCall(func() (KernelResult, error) { return k.CloneFromCLI(context.Background(), "https://github.com/example/project", "feature") })
				},
				want: `{"name":"launch.cli","action":{"kind":"clone-url","url":"https://github.com/example/project","branch":"feature"}}`,
			},
		{
			name: "application URL",
			call: func(k *Kernel) <-chan struct{ result KernelResult; err error } {
				return kernelCall(func() (KernelResult, error) { return k.DispatchApplicationURL(context.Background(), "gitowl-auth://oauth?code=one&state=two") })
				},
				want: `{"name":"launch.url","url":"gitowl-auth://oauth?code=one\u0026state=two"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			k, p := kernelPipes(t, nil)
			call := test.call(k)
			request := p.request(t)
			if request.Method != "kernel.command" || string(request.Params) != test.want {
				t.Fatalf("launch command = %s, want %s", request.Params, test.want)
			}
			p.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(1)})
			if result := receive(t, call); result.err != nil {
				t.Fatal(result.err)
			}
		})
	}
}

func TestDispatchApplicationURLRetainsEncodedOAuthData(t *testing.T) {
	k, p := kernelPipes(t, nil)
	call := kernelCall(func() (KernelResult, error) {
		return k.DispatchApplicationURL(context.Background(), "gitowl-auth://oauth?code=a%2Bb&state=x%2Fy")
	})
	request := p.request(t)
	var command struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(request.Params, &command); err != nil {
		t.Fatal(err)
	}
	if command.Name != "launch.url" || command.URL != "gitowl-auth://oauth?code=a%2Bb&state=x%2Fy" {
		t.Fatalf("OAuth URL changed before original parseAppURL: %+v", command)
	}
	p.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(1)})
	if result := receive(t, call); result.err != nil {
		t.Fatal(result.err)
	}
}
