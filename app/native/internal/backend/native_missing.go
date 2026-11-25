package backend

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
)

func (n *NativeOwner) recoverMissing(ctx context.Context, command nativeCommand) error {
	index := slices.IndexFunc(n.state.Repositories, func(r KernelRepository) bool { return r.KernelRepositoryReference == command.Repository })
	if index < 0 {
		return errors.New("repository is no longer registered")
	}
	var action string
	if err := json.Unmarshal(command.Action, &action); err != nil {
		return err
	}
	switch action {
	case "trust":
		missing := n.state.MissingRepository
		if missing == nil || missing.Repository.KernelRepositoryReference != command.Repository || missing.UnsafePath == nil {
			return errors.New("repository ownership warning is no longer active")
		}
		physical, err := filepath.EvalSymlinks(command.Repository.Path)
		if err != nil {
			return err
		}
		if _, err := n.git.Global(ctx, "config", "--global", "--add", "safe.directory", filepath.ToSlash(physical)); err != nil {
			return err
		}
		return n.selectRepository(ctx, command.Repository)
	case "check", "retry":
		return n.selectRepository(ctx, command.Repository)
	case "locate":
		paths, err := n.chooseDirectory(ctx)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			return nil
		}
		repository, err := n.git.Open(ctx, paths[0])
		if err != nil {
			return err
		}
		record := n.state.Repositories[index]
		record.Path = repository.Path
		record.Missing = false
		if err := n.store.SaveRepository(ctx, record); err != nil {
			return err
		}
		n.state.Repositories[index] = record
		return n.selectRepository(ctx, record.KernelRepositoryReference)
	case "remove":
		if err := n.store.RemoveRepository(ctx, command.Repository.ID); err != nil {
			return err
		}
		n.state.Repositories = slices.Delete(n.state.Repositories, index, index+1)
		n.state.MissingRepository = nil
		return nil
	default:
		return errors.New("unknown repository recovery action")
	}
}
