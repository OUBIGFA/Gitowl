package main

import "testing"

func TestDevelopmentProfilesCannotShareTheInstalledInstanceName(t *testing.T) {
	first, second := nativeProfileIdentity(`C:\fixture\one`), nativeProfileIdentity(`C:\fixture\two`)
	if first == "Gitowl" || first == second || first != nativeProfileIdentity(`C:\fixture\one`) {
		t.Fatal("isolated profiles do not own independent instance names")
	}
}
