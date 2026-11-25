package backend

import (
	"context"
	"errors"

	"path/filepath"
)

// Settled journals are recycled, including their original staging backup.
// Interrupted writes remain visible until the user resolves or archives them.
func (n *NativeOwner) refreshRecovery() error {
	if n.current == nil {
		return nil
	}
	records, err := n.current.CommitRecoveries()
	if err != nil {
		return err
	}
	pending := false
	for _, record := range records {
		// Constructor runs before the native UI can answer platform requests.
		// Surface retained data now; recycle it on the first user operation.
		if n.starting {
			pending = true
			continue
		}
		if record.Stage == "settled" || record.Stage == "preparing" || record.Stage == "rejected" {
			if err := n.recycle(record.Directory); err != nil {
				pending = true
			}
		} else {
			pending = true
		}
	}
	if pending {
		n.state.Workspace.Banner = &KernelWorkspaceBanner{ID: 1, Type: "native-recovery", LabelKey: "native.recoveryRequired", Actions: []string{"open"}}
	} else if n.state.Workspace.Banner != nil && n.state.Workspace.Banner.Type == "native-recovery" {
		n.state.Workspace.Banner = nil
	}
	return nil
}

func (n *NativeOwner) recoveryDialog() error {
	records, err := n.current.CommitRecoveries()
	if err != nil {
		return err
	}
	choices := []KernelDialogChoice{}
	for _, record := range records {
		choices = append(choices, KernelDialogChoice{ID: filepath.Base(record.Directory), Label: filepath.Base(record.Directory) + " · " + record.Stage})
	}
	if len(choices) == 0 {
		return errors.New("no retained commit recovery data")
	}
	n.form("NativeRecovery", "native.recoveryTitle", []KernelDialogField{{ID: "record", Kind: "select", LabelKey: "native.recoveryRecord", Value: choices[0].ID, Choices: choices}})
	n.state.CurrentPopup.DescriptionKey = "native.recoveryDescription"
	n.state.CurrentPopup.Actions = []KernelDialogAction{{ID: "settle", LabelKey: "native.recoverySettle", Primary: true, Fields: []string{"record"}}, {ID: "inspect", LabelKey: "native.recoveryInspect", Fields: []string{"record"}}, {ID: "archive", LabelKey: "native.recoveryArchive", Fields: []string{"record"}}, {ID: "cancel", LabelKey: "settings.cancel"}}
	return nil
}

func (n *NativeOwner) replyRecovery(ctx context.Context, reply KernelPopupReply) error {
	id, _ := reply.Values["record"].(string)
	records, err := n.current.CommitRecoveries()
	if err != nil {
		return err
	}
	directory := ""
	for _, record := range records {
		if filepath.Base(record.Directory) == id {
			directory = record.Directory
			break
		}
	}
	if directory == "" {
		return errors.New("recovery selection changed")
	}
	switch reply.Action {
	case "inspect":
		_, err = n.platform(ctx, "show-item-in-folder", filepath.Join(directory, "recovery.json"))
		return err
	case "settle":
		if err = n.current.RecoverCommit(ctx, directory); err != nil {
			return err
		}
		if err = n.recycle(directory); err != nil {
			return err
		}
	case "archive":
		n.confirm("native.recoveryArchive", directory, func(ctx context.Context) error { return n.recycle(directory) })
		return nil
	default:
		return errors.New("unknown recovery action")
	}
	n.state.CurrentPopup = nil
	n.state.Popups = []KernelPopup{}
	if err := n.refreshRecovery(); err != nil {
		return err
	}
	_, err = n.refresh(ctx, true)
	return err
}
