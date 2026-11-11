package backend

// Safe page presentations from the same original preferences draft. Native can
// select a visible page immediately without creating another settings owner.
type KernelDialogLayoutPage struct {
	ID      string               `json:"id"`
	Fields  []KernelDialogField  `json:"fields"`
	Actions []KernelDialogAction `json:"actions"`
}

// These are native UI primitives, not serialized React/HTML or raw kernel state.
// The original partition owns its allowed fields and actions. Action Fields is
// the exact value allow-list so read-only content/credentials are never echoed.
type KernelDialogField struct {
	Diff          *Diff                `json:"diff"`
	DiffPath      string               `json:"diffPath"`
	SideBySide    bool                 `json:"sideBySide"`
	ImageDiffType int                  `json:"imageDiffType"`
	Image         *Blob                `json:"image"`
	IconLibrary   string               `json:"iconLibrary"`
	ID            string               `json:"id"`
	LabelKey      string               `json:"labelKey"`
	Kind          string               `json:"kind"`
	Value         any                  `json:"value"`
	Text          string               `json:"text"`
	Placeholder   string               `json:"placeholder"`
	Choices       []KernelDialogChoice `json:"choices"`
	Required      bool                 `json:"required"`
	ReadOnly      bool                 `json:"readOnly"`
	Disabled      bool                 `json:"disabled"`
	Sensitive     bool                 `json:"sensitive"`
	Height        float32              `json:"height"`
	ChangeAction  string               `json:"changeAction"`
}

type KernelDialogChoice struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled"`
}

type KernelDialogAction struct {
	ID          string   `json:"id"`
	LabelKey    string   `json:"labelKey"`
	Primary     bool     `json:"primary"`
	Destructive bool     `json:"destructive"`
	Disabled    bool     `json:"disabled"`
	Fields      []string `json:"fields"`
}
