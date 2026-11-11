package main

import (
	"github.com/egoist/mygo"
	"testing"
)

func TestWindowAndQuitDoNotAbandonSubmittedCommit(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.savingVersion = true
	closing := &mygo.CloseEvent{}
	quitting := &mygo.QuitEvent{}
	a.guardWrite(&closing.Preventable)
	a.guardWrite(&quitting.Preventable)
	if !closing.DefaultPrevented() || !quitting.DefaultPrevented() || a.closed {
		t.Fatal("a submitted write must keep its process and response waiter alive")
	}
	a.savingVersion = false
	closing = &mygo.CloseEvent{}
	a.guardWrite(&closing.Preventable)
	if closing.DefaultPrevented() {
		t.Fatal("a settled write must allow closing")
	}
}
