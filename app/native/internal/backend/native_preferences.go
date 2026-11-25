package backend

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sqlite"
	"golang.org/x/text/language"
	"slices"
	"strings"
)

func (n *NativeOwner) preferences(page string) {
	var existingID *int
	if n.state.CurrentPopup != nil && n.state.CurrentPopup.Type == "Preferences" {
		existingID = n.state.CurrentPopup.ID
	}
	n.form("Preferences", "settings.title.Preferences", nil)
	popup := n.state.CurrentPopup
	if existingID != nil {
		popup.ID = existingID
		n.state.Popups = []KernelPopup{{ID: existingID, Type: "Preferences"}}
	}
	tools := []KernelDialogAction{}
	choices := []KernelDialogChoice{}
	for _, id := range []string{"accounts", "appearance", "git", "copilot"} {
		choices = append(choices, KernelDialogChoice{ID: id, Label: "settings.option." + id})
	}
	popup.Fields = []KernelDialogField{{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: page, Choices: choices, ChangeAction: "navigate"}}
	switch page {
	case "accounts":
		accountChoices := []KernelDialogChoice{{ID: "", Label: "settings.option.unassigned"}}
		for _, account := range n.accounts {
			accountChoices = append(accountChoices, KernelDialogChoice{ID: account.Endpoint + "\n" + account.Login, Label: account.Login + " · " + account.Endpoint})
		}
		selected := ""
		if n.state.Selected != nil && n.state.Selected.Repository.Account != nil {
			account := n.state.Selected.Repository.Account
			selected = account.Endpoint + "\n" + account.Login
		}
		popup.Fields = append(popup.Fields, KernelDialogField{ID: "account", Kind: "select", LabelKey: "settings.account", Value: selected, Choices: accountChoices})
		if n.state.Selected != nil {
			popup.Fields = append(popup.Fields, KernelDialogField{ID: "repositoryLogin", Kind: "select", LabelKey: "settings.repositoryLogin", Value: selected, Choices: accountChoices})
		}
		tools = append(tools, KernelDialogAction{ID: "signIn", LabelKey: "settings.signIn"}, KernelDialogAction{ID: "signOut", LabelKey: "settings.signOut", Fields: []string{"account"}})
	case "appearance":
		popup.Fields = append(popup.Fields, KernelDialogField{ID: "theme", Kind: "select", LabelKey: "settings.theme", Value: n.state.Appearance.Theme, Choices: []KernelDialogChoice{{ID: "system", Label: "settings.option.system"}, {ID: "light", Label: "settings.option.light"}, {ID: "dark", Label: "settings.option.dark"}}})
	case "git":
		name, email := "", ""
		if s := n.state.Selected; s != nil && s.CommitView.Author != nil {
			name, email = s.CommitView.Author.Name, s.CommitView.Author.Email
		}
		popup.Fields = append(popup.Fields, nativeField("committerName", "settings.committerName", name), nativeField("committerEmail", "settings.committerEmail", email))
	case "copilot":
		popup.Fields = append(popup.Fields, KernelDialogField{ID: "generationLanguage", LabelKey: "settings.generationLanguage", Kind: "select", Value: n.state.CommitGeneration.Language, Choices: []KernelDialogChoice{{ID: "zh-Hans", Label: "简体中文"}, {ID: "zh-Hant", Label: "繁體中文"}, {ID: "en", Label: "English"}, {ID: "auto", Label: "native.languageAuto"}}})
		popup.Fields = append(popup.Fields, KernelDialogField{ID: "generationSource", Kind: "select", LabelKey: "settings.generationSource", Value: n.state.CommitGeneration.Source, Choices: []KernelDialogChoice{{ID: "copilot", Label: "GitHub Copilot"}, {ID: "custom", Label: "settings.customProviders"}}})
		if account, err := n.assignedAccount(); err == nil {
			key := account.Endpoint + "\n" + account.Login
			catalog := n.copilotModels[key]
			selected, _ := n.store.Get(n.ctx, "native-copilot-model:"+account.Endpoint+"/"+account.Login)
			choices := []KernelDialogChoice{{ID: "", Label: "settings.option.none"}}
			for _, model := range catalog {
				choices = append(choices, KernelDialogChoice{ID: model.ID, Label: model.Name, Disabled: !model.Enabled || !model.Available})
				popup.Fields = append(popup.Fields, KernelDialogField{ID: "enabled:" + model.ID, Kind: "checkbox", LabelKey: "settings.generationModel", Label: model.Name, Value: model.Enabled, Disabled: !model.Available, ChangeAction: "change"})
			}
			popup.Fields = append(popup.Fields, KernelDialogField{ID: "generationModel", Kind: "select", LabelKey: "settings.generationModel", Value: selected, Choices: choices})
		} else {
			popup.Fields = append(popup.Fields, KernelDialogField{ID: "accountHint", Kind: "readonly", ReadOnly: true, LabelKey: "native.copilotAccountHint", Text: ""})
		}
		_, accountErr := n.assignedAccount()
		tools = append(tools, KernelDialogAction{ID: "refreshCopilot", LabelKey: "native.refreshCopilotModels", Disabled: accountErr != nil}, KernelDialogAction{ID: "customProviders", LabelKey: "settings.customProviders"})
	}
	fields := []string{"tab"}
	for _, field := range popup.Fields[1:] {
		fields = append(fields, field.ID)
	}
	popup.Actions = []KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true, Fields: fields}, {ID: "navigate", LabelKey: "settings.tab", Fields: []string{"tab"}}, {ID: "cancel", LabelKey: "settings.cancel"}}
	popup.Actions = append(popup.Actions, tools...)
	if page == "copilot" {
		popup.Actions = append(popup.Actions, KernelDialogAction{ID: "change", LabelKey: "settings.generationModel", Fields: fields})
	}
}
func (n *NativeOwner) replyPreferences(ctx context.Context, reply KernelPopupReply) error {
	text := func(key string) string { value, _ := reply.Values[key].(string); return value }
	if reply.Action == "navigate" {
		n.preferences(text("tab"))
		return nil
	}
	if reply.Action == "signIn" {
		return n.beginSignIn(ctx, "https://api.github.com")
	}
	if reply.Action == "signOut" {
		endpoint, login, ok := strings.Cut(text("account"), "\n")
		if !ok {
			return errors.New("select an account")
		}
		if err := mygo.Secrets.Service(credentialService(n.credentialNamespace, endpoint)).Delete(login); err != nil && !errors.Is(err, mygo.ErrSecretNotFound) {
			return err
		}
		n.accounts = slices.DeleteFunc(n.accounts, func(a nativeAccount) bool { return a.Endpoint == endpoint && a.Login == login })
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
		n.preferences("accounts")
		return nil
	}
	if reply.Action == "refreshCopilot" {
		if err := n.refreshCopilotModels(ctx); err != nil {
			return err
		}
		n.preferences("copilot")
		return nil
	}
	if reply.Action == "customProviders" {
		n.preferences("copilot")
		return nil
	}
	if reply.Action == "change" {
		account, err := n.assignedAccount()
		if err != nil {
			return err
		}
		key := account.Endpoint + "\n" + account.Login
		catalog := slices.Clone(n.copilotModels[key])
		for i := range catalog {
			if enabled, ok := reply.Values["enabled:"+catalog[i].ID].(bool); ok {
				catalog[i].Enabled = enabled
			}
		}
		data, err := json.Marshal(catalog)
		if err != nil {
			return err
		}
		if err := n.store.Set(ctx, "native-copilot-catalog:"+key, string(data)); err != nil {
			return err
		}
		n.copilotModels[key] = catalog
		selected, err := n.store.Get(ctx, "native-copilot-model:"+account.Endpoint+"/"+account.Login)
		if err != nil {
			return err
		}
		if selected != "" && !slices.ContainsFunc(catalog, func(m ProviderModel) bool { return m.ID == selected && m.Enabled && m.Available }) {
			if err := n.store.Set(ctx, "native-copilot-model:"+account.Endpoint+"/"+account.Login, ""); err != nil {
				return err
			}
		}
		n.preferences("copilot")
		return nil
	}
	if reply.Action != "save" {
		return errors.New("unknown preferences action")
	}
	if theme := text("theme"); theme != "" {
		if theme != "system" && theme != "light" && theme != "dark" {
			return errors.New("unknown theme")
		}
		if err := n.store.Set(ctx, "theme", theme); err != nil {
			return err
		}
		n.state.Appearance.Theme = theme
	}
	if language := text("generationLanguage"); language != "" {
		if language != "auto" {
			if _, err := languagepkgParse(language); err != nil {
				return errors.New("invalid generation language")
			}
		}
		if err := n.store.Set(ctx, "gitowl-commit-generation-language", language); err != nil {
			return err
		}
		n.state.CommitGeneration.Language = language
	}
	if value, present := reply.Values["repositoryLogin"]; present && n.state.Selected != nil {
		assignment, _ := value.(string)
		var account *KernelAccountIdentity
		if assignment != "" {
			endpoint, login, ok := strings.Cut(assignment, "\n")
			if !ok || !slices.ContainsFunc(n.accounts, func(a nativeAccount) bool { return a.Endpoint == endpoint && a.Login == login }) {
				return errors.New("selected account is unavailable")
			}
			account = &KernelAccountIdentity{Endpoint: endpoint, Login: login}
		}
		r := n.state.Selected.Repository
		r.Account = account
		r.Login = nil
		if account != nil {
			r.Login = &account.Login
		}
		if err := n.store.SaveRepository(ctx, r); err != nil {
			return err
		}
		n.state.Selected.Repository = r
		for i := range n.state.Repositories {
			if n.state.Repositories[i].ID == r.ID {
				n.state.Repositories[i] = r
			}
		}
	}
	if source := text("generationSource"); source != "" {
		if source != "copilot" && source != "custom" {
			return errors.New("invalid generation source")
		}
		if err := n.store.Set(ctx, "gitowl-commit-generation-source", source); err != nil {
			return err
		}
		n.state.CommitGeneration.Source = source
	}
	if account, err := n.assignedAccount(); err == nil {
		key := account.Endpoint + "\n" + account.Login
		catalog := slices.Clone(n.copilotModels[key])
		for i := range catalog {
			if enabled, ok := reply.Values["enabled:"+catalog[i].ID].(bool); ok {
				catalog[i].Enabled = enabled
			}
		}
		statements := []sqlite.Statement{}
		if model, present := reply.Values["generationModel"]; present {
			value, _ := model.(string)
			if value != "" && !slices.ContainsFunc(catalog, func(m ProviderModel) bool { return m.ID == value && m.Enabled && m.Available }) {
				return errors.New("select an enabled Copilot model")
			}
			statements = append(statements, sqlite.Statement{SQL: "INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", Args: []any{"native-copilot-model:" + account.Endpoint + "/" + account.Login, value}})
		}
		data, err := json.Marshal(catalog)
		if err != nil {
			return err
		}
		statements = append(statements, sqlite.Statement{SQL: "INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", Args: []any{"native-copilot-catalog:" + key, string(data)}})
		if _, err := n.store.db.Transaction(ctx, statements); err != nil {
			return err
		}
		n.copilotModels[key] = catalog
	}
	if name := text("committerName"); name != "" {
		if _, err := n.git.Global(ctx, "config", "--global", "user.name", name); err != nil {
			return err
		}
	}
	if email := text("committerEmail"); email != "" {
		if _, err := n.git.Global(ctx, "config", "--global", "user.email", email); err != nil {
			return err
		}
	}
	n.state.CurrentPopup = nil
	n.state.Popups = []KernelPopup{}
	return nil
}

func languagepkgParse(value string) (language.Tag, error) { return language.Parse(value) }
