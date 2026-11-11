package main

import "github.com/egoist/mygo/ui"

// MyGo's painted tree retains a pointer into ListState until the next frame.
// Reset the owner, never the bound value: Windows accessibility may still read
// the old frame after a repository/model change has scheduled its replacement.
type nativeListState struct {
	value *ui.ListState
}

func (s *nativeListState) list() *ui.ListState {
	if s.value == nil {
		s.value = &ui.ListState{}
	}
	return s.value
}
