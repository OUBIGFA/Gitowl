package backend

import "context"

// KernelProviderSelection is the compound custom-model identity in the original
// AppStore projection. It is not a research SDK account selection.
type KernelProviderSelection struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

type KernelEnabledProviderModel struct {
	KernelProviderSelection
	ProviderName string `json:"providerName"`
	ModelName    string `json:"modelName"`
}

// KernelGenerationSettings mirrors IAppState.commitGeneration metadata only.
// Copilot accounts/models remain in their original account-scoped kernel stores;
// no credential or separate SDK login session is represented here.
type KernelGenerationSettings struct {
	Providers     []Provider                   `json:"providers"`
	EnabledModels []KernelEnabledProviderModel `json:"enabledModels"`
	Selection     *KernelProviderSelection     `json:"selection"`
	Source        string                       `json:"source"`
	Language      string                       `json:"language"`
}

// Provider metadata never includes its credential. Display names are mutable;
// IDs and model IDs identify configured choices across provider renames.
type ProviderSettings struct {
	ID                    string  `json:"id,omitempty"`
	Name                  string  `json:"name"`
	Format                string  `json:"format"`
	BaseURL               string  `json:"baseUrl"`
	AuthKind              string  `json:"authKind"`
	RequestTimeoutSeconds float64 `json:"requestTimeoutSeconds,omitempty"`
}

type ProviderModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
}

type Provider struct {
	ProviderSettings
	Models []ProviderModel `json:"models"`
}

type ModelSelection struct {
	Kind       string `json:"kind"`
	ProviderID string `json:"providerId,omitempty"`
	AccountID  string `json:"accountId,omitempty"`
	ModelID    string `json:"modelId"`
}

type ModelOption struct {
	Selection  ModelSelection `json:"selection"`
	SourceName string         `json:"sourceName"`
	ModelName  string         `json:"modelName"`
}

type CopilotAccount struct {
	SelectionID string `json:"selectionId"`
	Host        string `json:"host"`
	Login       string `json:"login"`
	Kind        string `json:"kind"`
	Active      bool   `json:"active"`
}

type CopilotCatalog struct {
	Account CopilotAccount  `json:"account"`
	Models  []ProviderModel `json:"models"`
}

type GenerationSettings struct {
	Providers       []Provider       `json:"providers"`
	CopilotCatalogs []CopilotCatalog `json:"copilotCatalogs"`
	Models          []ModelOption    `json:"models"`
	Selection       *ModelSelection  `json:"selection"`
	Language        string           `json:"language"`
}

type GeneratedMessage struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Selection   ModelSelection `json:"selection"`
	Language    string         `json:"language"`
}

type CopilotStatus struct {
	IsAuthenticated bool            `json:"isAuthenticated"`
	ActiveAccount   *CopilotAccount `json:"activeAccount"`
	AccountCount    int             `json:"accountCount"`
	CopilotPlan     *string         `json:"copilotPlan"`
}

type CopilotLoginResult struct {
	Status   string           `json:"status"`
	Host     string           `json:"host,omitempty"`
	Login    string           `json:"login,omitempty"`
	Accounts []CopilotAccount `json:"accounts,omitempty"`
}

type CopilotLoginStep struct {
	Kind    string              `json:"kind"`
	URL     string              `json:"url,omitempty"`
	Prompt  string              `json:"prompt,omitempty"`
	Message string              `json:"message,omitempty"`
	Result  *CopilotLoginResult `json:"result,omitempty"`
}

type CopilotLoginFlow struct {
	FlowID string           `json:"flowId"`
	Step   CopilotLoginStep `json:"step"`
}

// These wrappers admit only named original Dispatcher commands and return its
// full snapshot. None can reach the prototype generation/account service.
func (k *Kernel) SaveGenerationProvider(ctx context.Context, provider ProviderSettings, secret *string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name     string           `json:"name"`
		Provider ProviderSettings `json:"provider"`
		Secret   *string          `json:"secret,omitempty"`
	}{"generation.provider.save", provider, secret})
}

func (k *Kernel) RenameGenerationProvider(ctx context.Context, id, name string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name         string `json:"name"`
		ID           string `json:"id"`
		ProviderName string `json:"providerName"`
	}{"generation.provider.rename", id, name})
}

func (k *Kernel) DeleteGenerationProvider(ctx context.Context, id string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}{"generation.provider.delete", id})
}

func (k *Kernel) RefreshGenerationProviderModels(ctx context.Context, id string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}{"generation.provider.models.refresh", id})
}

func (k *Kernel) SetGenerationProviderModelEnabled(ctx context.Context, provider, model string, enabled bool) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string `json:"name"`
		ProviderID string `json:"providerId"`
		ModelID    string `json:"modelId"`
		Enabled    bool   `json:"enabled"`
	}{"generation.provider.models.enabled", provider, model, enabled})
}

// Deployment names are explicit Azure input, never a guessed model catalog.
func (k *Kernel) SetGenerationDeployments(ctx context.Context, id string, models []ProviderDeployment) (KernelResult, error) {
	return k.command(ctx, struct {
		Name   string               `json:"name"`
		ID     string               `json:"id"`
		Models []ProviderDeployment `json:"models"`
	}{"generation.provider.deployments.set", id, models})
}

type ProviderDeployment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (k *Kernel) SelectGenerationModel(ctx context.Context, source string, selection *KernelProviderSelection) (KernelResult, error) {
	return k.command(ctx, struct {
		Name      string                   `json:"name"`
		Source    string                   `json:"source"`
		Selection *KernelProviderSelection `json:"selection"`
	}{"generation.select", source, selection})
}

func (k *Kernel) SetCommitGenerationLanguage(ctx context.Context, language string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name     string `json:"name"`
		Language string `json:"language"`
	}{"generation.language", language})
}

// Opens the ORIGINAL Preferences popup/account-scoped Copilot model controls.
func (k *Kernel) OpenGenerationCopilotSettings(ctx context.Context) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
	}{"generation.copilot.settings"})
}

// GenerationBackend is separate from read-only repository test doubles. The
// handshake still decides whether the running service supplies these methods.
type GenerationBackend interface {
	GenerationSettings(context.Context) (GenerationSettings, error)
	SaveProvider(context.Context, ProviderSettings, *string) (GenerationSettings, error)
	RenameProvider(context.Context, string, string) (GenerationSettings, error)
	DeleteProvider(context.Context, string) (GenerationSettings, error)
	RefreshProviderModels(context.Context, string) (GenerationSettings, error)
	SetProviderModelEnabled(context.Context, string, string, bool) (GenerationSettings, error)
	AddProviderModel(context.Context, string, string, string) (GenerationSettings, error)
	SetGenerationModel(context.Context, *ModelSelection) (GenerationSettings, error)
	SetGenerationLanguage(context.Context, string) (GenerationSettings, error)
	GenerateMessage(context.Context, int, []string, bool) (GeneratedMessage, error)
	CopilotStatus(context.Context) (CopilotStatus, error)
	CopilotAccounts(context.Context) ([]CopilotAccount, error)
	BeginCopilotLogin(context.Context) (CopilotLoginFlow, error)
	AdvanceCopilotLogin(context.Context, string, string) (CopilotLoginStep, error)
	CancelCopilotLogin(context.Context, string) error
	LogoutCopilot(context.Context, string) (GenerationSettings, error)
	RefreshCopilotModels(context.Context, string) (GenerationSettings, error)
	SetCopilotModelEnabled(context.Context, string, string, bool) (GenerationSettings, error)
}

func (c *Client) generationSettingCall(ctx context.Context, method string, params any) (GenerationSettings, error) {
	var out GenerationSettings
	err := c.Call(ctx, method, params, &out)
	if err == nil && (out.Providers == nil || out.Models == nil || out.CopilotCatalogs == nil || out.Language == "") {
		err = c.invalid(method, "missing generation settings")
	}
	return out, err
}

func (c *Client) GenerationSettings(ctx context.Context) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "generation.settings", nil)
}
func (c *Client) SaveProvider(ctx context.Context, p ProviderSettings, secret *string) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "providers.save", struct {
		Provider ProviderSettings `json:"provider"`
		Secret   *string          `json:"secret,omitempty"`
	}{p, secret})
}
func (c *Client) RenameProvider(ctx context.Context, id, name string) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "providers.rename", map[string]string{"id": id, "name": name})
}
func (c *Client) DeleteProvider(ctx context.Context, id string) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "providers.delete", map[string]string{"id": id})
}
func (c *Client) RefreshProviderModels(ctx context.Context, id string) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "providers.models.refresh", map[string]string{"id": id})
}
func (c *Client) SetProviderModelEnabled(ctx context.Context, id, model string, enabled bool) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "providers.models.setEnabled", struct {
		ID      string `json:"id"`
		ModelID string `json:"modelId"`
		Enabled bool   `json:"enabled"`
	}{id, model, enabled})
}
func (c *Client) AddProviderModel(ctx context.Context, id, model, name string) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "providers.models.add", map[string]string{"id": id, "modelId": model, "name": name})
}
func (c *Client) SetGenerationModel(ctx context.Context, selection *ModelSelection) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "generation.select", struct {
		Selection *ModelSelection `json:"selection"`
	}{selection})
}
func (c *Client) SetGenerationLanguage(ctx context.Context, language string) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "generation.language", map[string]string{"language": language})
}
func (c *Client) GenerateMessage(ctx context.Context, id int, paths []string, consent bool) (GeneratedMessage, error) {
	var out GeneratedMessage
	err := c.Call(ctx, "repository.generateMessage", struct {
		ID      int      `json:"id"`
		Paths   []string `json:"paths"`
		Consent bool     `json:"consent"`
	}{id, paths, consent}, &out)
	if err == nil && out.Title == "" {
		err = c.invalid("repository.generateMessage", "missing generated title")
	}
	return out, err
}
func (c *Client) CopilotStatus(ctx context.Context) (CopilotStatus, error) {
	var out CopilotStatus
	err := c.Call(ctx, "copilot.status", nil, &out)
	return out, err
}
func (c *Client) CopilotAccounts(ctx context.Context) ([]CopilotAccount, error) {
	var out struct {
		Accounts []CopilotAccount `json:"accounts"`
	}
	err := c.Call(ctx, "copilot.accounts", nil, &out)
	return out.Accounts, err
}
func (c *Client) BeginCopilotLogin(ctx context.Context) (CopilotLoginFlow, error) {
	var out CopilotLoginFlow
	err := c.Call(ctx, "copilot.login.begin", nil, &out)
	return out, err
}
func (c *Client) AdvanceCopilotLogin(ctx context.Context, id, input string) (CopilotLoginStep, error) {
	var out CopilotLoginStep
	err := c.Call(ctx, "copilot.login.advance", map[string]string{"flowId": id, "input": input}, &out)
	return out, err
}
func (c *Client) CancelCopilotLogin(ctx context.Context, id string) error {
	var out struct {
		Cancelled bool `json:"cancelled"`
	}
	return c.Call(ctx, "copilot.login.cancel", map[string]string{"flowId": id}, &out)
}
func (c *Client) LogoutCopilot(ctx context.Context, id string) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "copilot.logout", map[string]string{"selectionId": id})
}
func (c *Client) RefreshCopilotModels(ctx context.Context, id string) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "copilot.models.refresh", map[string]string{"selectionId": id})
}
func (c *Client) SetCopilotModelEnabled(ctx context.Context, id, model string, enabled bool) (GenerationSettings, error) {
	return c.generationSettingCall(ctx, "copilot.models.setEnabled", struct {
		AccountID string `json:"selectionId"`
		ModelID   string `json:"modelId"`
		Enabled   bool   `json:"enabled"`
	}{id, model, enabled})
}
