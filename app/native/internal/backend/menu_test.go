package backend

import (
	"context"
	"testing"
)

func TestMenuExecutionSendsOnlyOriginalOpaqueID(t *testing.T) {
	k, pipe := kernelPipes(t, nil)
	call := kernelCall(func() (KernelResult, error) { return k.ExecuteMenu(context.Background(), "push") })
	request := pipe.request(t)
	if request.Method != "kernel.command" || string(request.Params) != `{"name":"menu.execute","id":"push"}` {
		t.Fatalf("menu command must not reflect Dispatcher methods or payloads: %s %s", request.Method, request.Params)
	}
	pipe.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(1)})
	if result := receive(t, call); result.err != nil {
		t.Fatal(result.err)
	}
}
