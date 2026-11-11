package backend

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestWorkspaceActionWireHasOnlyExplicitIntentFields(t *testing.T) {
	tests := []struct {
		action KernelWorkspaceAction
		want   map[string]any
	}{
		{KernelWorkspaceAction{Type: "worktree.select", Path: "fixture/linked", Option: "must-not-leak"}, map[string]any{"type": "worktree.select", "path": "fixture/linked"}},
		{KernelWorkspaceAction{Type: "stash.discard", SHA: "stash-sha", Path: "must-not-leak"}, map[string]any{"type": "stash.discard", "sha": "stash-sha"}},
		{KernelWorkspaceAction{Type: "diff.option", Option: "whitespace", Value: false}, map[string]any{"type": "diff.option", "option": "whitespace", "value": false}},
		{KernelWorkspaceAction{Type: "history.select"}, map[string]any{"type": "history.select", "shas": []string{}}},
		{KernelWorkspaceAction{Type: "history.reorder", SHAs: []string{"sha"}}, map[string]any{"type": "history.reorder", "shas": []string{"sha"}, "beforeSHA": (*string)(nil)}},
		{KernelWorkspaceAction{Type: "navigation.filter", Option: "branches", Path: "ftr"}, map[string]any{"type": "navigation.filter", "scope": "branches", "text": "ftr"}},
		{KernelWorkspaceAction{Type: "compare.operation", Option: "rebase"}, map[string]any{"type": "compare.operation", "operation": "rebase"}},
	}
	for _, test := range tests {
		got, err := test.action.wire()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: got %#v want %#v", test.action.Type, got, test.want)
		}
		if _, err := json.Marshal(got); err != nil {
			t.Fatal(err)
		}
	}
	// An unknown operation fails before touching even an initialized transport.
	if _, err := (&Kernel{}).WorkspaceAction(context.Background(), KernelRepositoryReference{}, KernelWorkspaceAction{Type: "dispatcher.deleteWorktree"}); err == nil {
		t.Fatal("unrecognized backend method was admitted")
	}
}

func TestWorkspaceMenuAndExpansionDTOJSONRoundTrip(t *testing.T) {
	text := "@@ -1 +1 @@\n-old\n+new\n"
	original := KernelWorkspace{Repository: &KernelRepositoryReference{ID: 1, Path: "fixture/repo"}, Menus: []KernelWorkspaceMenu{{Target: KernelWorkspaceMenuTarget{Kind: "worktree", ID: "fixture/tree"}, Lease: "opaque", Items: []KernelWorkspaceMenuItem{{LabelKey: "workspace.delete", Enabled: false}}}}, TextDiff: &KernelWorkspaceTextDiff{FileID: "Modified+file", Identity: "patch-lease", Prepared: true, CanExpand: true, ExpandedText: &text, Selection: &KernelDiffSelection{FileID: "Modified+file", Identity: "original-patch-identity"}}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded KernelWorkspace
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatal("workspace transport lost a disabled action, opaque lease, or original diff identity")
	}
}
