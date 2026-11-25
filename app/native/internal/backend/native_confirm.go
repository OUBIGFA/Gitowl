package backend

import (
	"context"
	"errors"
)

func (n *NativeOwner) confirm(title, description string, action func(context.Context) error) {
	n.form("NativeConfirm", title, []KernelDialogField{{ID: "description", LabelKey: title, Kind: "readonly", ReadOnly: true, Text: description}})
	n.state.CurrentPopup.Actions[0].Destructive = true
	n.confirmAction = action
}
func (n *NativeOwner) confirmReply(ctx context.Context, reply KernelPopupReply) error {
	if reply.Action != "save" {
		n.confirmAction = nil
		n.state.CurrentPopup = nil
		n.state.Popups = []KernelPopup{}
		return nil
	}
	if n.confirmAction == nil {
		return errors.New("confirmation has expired")
	}
	action := n.confirmAction
	n.confirmAction = nil
	if err := action(ctx); err != nil {
		_, refreshErr := n.refresh(ctx, true)
		return errors.Join(err, refreshErr)
	}
	n.state.CurrentPopup = nil
	n.state.Popups = []KernelPopup{}
	_, err := n.refresh(ctx, true)
	return errors.Join(err, n.refreshRecovery())
}
