package backend

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestKernelGenerationUsesNamedOriginalCommands(t *testing.T) {
	provider := ProviderSettings{ID: "fixture-provider", Name: "Fixture provider", Format: "ollama", BaseURL: "http://localhost:11434", AuthKind: "bearer", RequestTimeoutSeconds: 60}
	secret := "fake-credential-for-owned-pipe-test"
	selection := &KernelProviderSelection{ProviderID: provider.ID, ModelID: "fixture:latest"}
	for _, scenario := range []struct {
		name string
		call func(*Kernel) (KernelResult, error)
		want map[string]any
	}{
		{"save", func(k *Kernel) (KernelResult, error) {
			return k.SaveGenerationProvider(context.Background(), provider, &secret)
		}, map[string]any{"name": "generation.provider.save", "provider": provider, "secret": secret}},
		{"rename", func(k *Kernel) (KernelResult, error) {
			return k.RenameGenerationProvider(context.Background(), provider.ID, "Renamed")
		}, map[string]any{"name": "generation.provider.rename", "id": provider.ID, "providerName": "Renamed"}},
		{"delete", func(k *Kernel) (KernelResult, error) {
			return k.DeleteGenerationProvider(context.Background(), provider.ID)
		}, map[string]any{"name": "generation.provider.delete", "id": provider.ID}},
		{"refresh", func(k *Kernel) (KernelResult, error) {
			return k.RefreshGenerationProviderModels(context.Background(), provider.ID)
		}, map[string]any{"name": "generation.provider.models.refresh", "id": provider.ID}},
		{"enabled", func(k *Kernel) (KernelResult, error) {
			return k.SetGenerationProviderModelEnabled(context.Background(), provider.ID, selection.ModelID, false)
		}, map[string]any{"name": "generation.provider.models.enabled", "providerId": provider.ID, "modelId": selection.ModelID, "enabled": false}},
		{"deployments", func(k *Kernel) (KernelResult, error) {
			return k.SetGenerationDeployments(context.Background(), provider.ID, []ProviderDeployment{{ID: "deployment", Name: "Explicit deployment"}})
		}, map[string]any{"name": "generation.provider.deployments.set", "id": provider.ID, "models": []ProviderDeployment{{ID: "deployment", Name: "Explicit deployment"}}}},
		{"custom", func(k *Kernel) (KernelResult, error) {
			return k.SelectGenerationModel(context.Background(), "custom", selection)
		}, map[string]any{"name": "generation.select", "source": "custom", "selection": selection}},
		{"clear-custom", func(k *Kernel) (KernelResult, error) {
			return k.SelectGenerationModel(context.Background(), "custom", nil)
		}, map[string]any{"name": "generation.select", "source": "custom", "selection": nil}},
		{"copilot", func(k *Kernel) (KernelResult, error) {
			return k.SelectGenerationModel(context.Background(), "copilot", nil)
		}, map[string]any{"name": "generation.select", "source": "copilot", "selection": nil}},
		{"language", func(k *Kernel) (KernelResult, error) {
			return k.SetCommitGenerationLanguage(context.Background(), "sr-Latn-RS")
		}, map[string]any{"name": "generation.language", "language": "sr-Latn-RS"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			k, p := kernelPipes(t, nil)
			call := kernelCall(func() (KernelResult, error) { return scenario.call(k) })
			request := p.request(t)
			if request.Method != "kernel.command" {
				t.Fatalf("generation escaped the original kernel: %s", request.Method)
			}
			var got, want any
			if err := json.Unmarshal(request.Params, &got); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(scenario.want)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("named command contract changed: %s != %s", request.Params, encoded)
			}
			// No secondary repository, account or generation receipt is invented.
			p.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(2)})
			result := receive(t, call)
			if result.err != nil || result.result.Snapshot.Revision != 2 || result.result.Snapshot.Core != "desktop/desktop" {
				t.Fatalf("original generation snapshot lost: %+v", result)
			}
		})
	}
}

func TestKernelProviderSaveOmitsRetainedCredential(t *testing.T) {
	k, p := kernelPipes(t, nil)
	call := kernelCall(func() (KernelResult, error) {
		return k.SaveGenerationProvider(context.Background(), ProviderSettings{ID: "fixture", Name: "Keep credential", Format: "ollama", BaseURL: "http://localhost:11434", AuthKind: "bearer"}, nil)
	})
	request := p.request(t)
	var command map[string]json.RawMessage
	if err := json.Unmarshal(request.Params, &command); err != nil {
		t.Fatal(err)
	}
	if _, present := command["secret"]; present {
		t.Fatal("a retained OS credential was serialized into the native command")
	}
	p.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(1)})
	if result := receive(t, call); result.err != nil {
		t.Fatal(result.err)
	}
}
