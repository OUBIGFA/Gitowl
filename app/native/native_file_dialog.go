package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo"
	"slices"
)

// Map original options to MyGo's system picker and attach it to the actual
// foreground owner. An empty default path means the OS chooses its default.
func showNativeFileDialog(win *mygo.Window, request backend.KernelFileDialog) ([]string, error) {
	o := request.Options
	filters := make([]mygo.FileFilter, len(o.Filters))
	for i, f := range o.Filters {
		filters[i] = mygo.FileFilter{Name: f.Name, Extensions: f.Extensions}
	}
	has := func(value string) bool { return slices.Contains(o.Properties, value) }
	if request.Kind == "save" {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: win, Title: o.Title, DefaultPath: o.DefaultPath, ButtonLabel: o.ButtonLabel, Message: o.Message, NameFieldLabel: o.NameFieldLabel, Filters: filters, ShowHiddenFiles: has("showHiddenFiles"), CreateDirectories: has("createDirectory"), TreatPackagesAsDirectories: has("treatPackageAsDirectory")})
		if path == "" {
			return nil, err
		}
		return []string{path}, err
	}
	return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: o.Title, DefaultPath: o.DefaultPath, ButtonLabel: o.ButtonLabel, Message: o.Message, Filters: filters, Directory: has("openDirectory"), Multiple: has("multiSelections"), ShowHiddenFiles: has("showHiddenFiles"), CreateDirectories: has("createDirectory"), TreatPackagesAsDirectories: has("treatPackageAsDirectory")})
}
