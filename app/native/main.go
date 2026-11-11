// Gitowl's visible frontend is MyGo native UI. Its production launch connects
// to the original Desktop Electron kernel; the old reduced Node/Git service is
// no longer a launcher option or a substitute for the full upstream runtime.
package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// BuildNumber is supplied by the parent build with -X main.BuildNumber=... .
var BuildNumber = "dev"

func main() {
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	options, err := parseNativeLaunchOptions(os.Args[1:], workingDirectory)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Gitowl usage: gitowl [-kernel main.js] [-electron electron.exe] [repository-path]", err)
		os.Exit(2)
	}
	if err := run(options.entry, options.electron, options.openPath, options.profile, options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(entry, electron, initialPath, profile string, initialLaunch nativeLaunchOptions) error {
	// Respect mygo dev's isolated name and its linked product identity. Plain
	// go builds have no identity; normal packaged name/version never change.
	packaged := mygo.App.Version() != ""
	if !packaged {
		mygo.App.SetName("Gitowl")
		mygo.App.SetVersion("0.0.1")
	}
	if profile != "" {
		if !mygo.IsDev() || !filepath.IsAbs(profile) {
			return errors.New("isolated --user-data is an absolute development-only path")
		}
		mygo.App.SetPath(mygo.PathUserData, profile)
	}
	// Take the visible application's lock before spawning its one kernel. A
	// second presenter cannot create a second repository/account state owner.
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	resources, resourceErr := mygo.App.Path(mygo.PathResources)
	userData, userDataErr := mygo.App.Path(mygo.PathUserData)
	config, configErr := backend.ResolveKernelConfig(resources, userData, entry, electron)
	logo, logoErr := loadLogo(resources)
	a := newCoreApp(nil, logo)
	a.serviceErr, a.logoErr = errors.Join(resourceErr, userDataErr, configErr), logoErr
	a.windowPreferencesPath = filepath.Join(userData, "window-preferences.json")
	a.windowPreferences, a.windowPreferenceErr = readWindowPreferences(a.windowPreferencesPath)
	defer a.close()
	var core *backend.Kernel
	defer func() {
		if core != nil {
			if err := core.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "original Desktop kernel shutdown:", err)
			}
		}
	}()
	var win *mygo.Window
	launches := &nativeLaunchRouter{dispatch: a.routeCoreLaunch}
	if packaged {
		// These are Gitowl's own schemes from mygo.json, not shared Desktop
		// callback registrations. Unlinked go builds have no package schemes.
		launches.automaticSchemes = []string{"gitowl", "gitowl-auth"}
	}
	if configErr == nil {
		callback, err := registerNativeOAuthCallback(config.Entry, mygo.App.RegisterURLScheme)
		if callback != "" {
			launches.automaticSchemes = append(launches.automaticSchemes, callback)
		}
		if err != nil {
			a.openErr = fmt.Errorf("register original OAuth callback: %w", err)
		}
	}
	mygo.App.OnSecondInstance(func(arguments []string, workingDirectory string) {
		if win != nil {
			win.Show()
			win.Restore()
			win.Focus()
		}
		if err := launches.secondInstance(arguments, workingDirectory); err != nil {
			a.openErr = err
		}
	})
	mygo.App.OnOpenURL(launches.openURL)
	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: locales.AppName(), Width: 1180, Height: 760, MinWidth: 900, MinHeight: 620,
			StateKey: "repository-browser", TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 48,
			AutoHideMenuBar: true, Content: ui.View(a.view),
		})
		a.setCoreTheme = func(source string) { mygo.Theme.SetSource(mygo.ThemeSource(source)) }
		a.update, a.setLocale, a.openExternal = win.Update, mygo.App.SetLocale, mygo.Shell.OpenExternal
		a.localeOverride = mygo.App.LocaleOverride
		a.settingsWorkArea = func() (float32, float32) {
			outer, content := win.Bounds(), win.ContentBounds()
			area := mygo.Screen.DisplayMatching(outer).WorkArea
			return float32(area.Width - outer.Width + content.Width), float32(area.Height - outer.Height + content.Height)
		}
		a.expandSettings = func(width, height float32) {
			// Resize outside the view build and keep the entire native frame on
			// its current monitor. Never shrink a window the user has enlarged.
			win.Update(func() {
				outer, content := win.Bounds(), win.ContentBounds()
				area := mygo.Screen.DisplayMatching(outer).WorkArea
				next := outer
				next.Width = min(area.Width, max(outer.Width, int(math.Ceil(float64(width)))+outer.Width-content.Width))
				next.Height = min(area.Height, max(outer.Height, int(math.Ceil(float64(height)))+outer.Height-content.Height))
				next.X = max(area.X, min(next.X, area.X+area.Width-next.Width))
				next.Y = max(area.Y, min(next.Y, area.Y+area.Height-next.Height))
				if next != outer {
					win.SetBounds(next)
				}
			})
		}
		a.chooseDirectory = func(title, path string) ([]string, error) {
			return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: title, DefaultPath: path, Directory: true})
		}
		a.coreWindowState = func() backend.KernelWindowState {
			state := "normal"
			if win.IsFullScreen() {
				state = "full-screen"
			} else if win.IsMinimized() {
				state = "minimized"
			} else if win.IsMaximized() {
				state = "maximized"
			} else if !win.IsVisible() {
				state = "hidden"
			}
			return backend.KernelWindowState{Visible: win.IsVisible(), Focused: win.IsFocused(), Minimized: win.IsMinimized(), State: state, Zoom: a.coreZoom}
		}
		a.coreWindowAction = func(action string) {
			switch action {
			case "show":
				win.Show()
			case "focus":
				win.Focus()
			case "restore":
				win.Restore()
			case "minimize":
				win.Minimize()
			case "maximize":
				win.Maximize()
			case "unmaximize":
				win.Unmaximize()
			case "close":
				win.Close()
			case "select-all":
				if !a.workspacePresentation("select-all") {
					win.SelectAll()
				}
				win.Invalidate()
			case "attention":
				win.FlashFrame(true)
			default:
				// Do not silently report an unimplemented native window command as
				// applied. Editing/menu parity is not yet an accepted contract.
				a.serviceErr = fmt.Errorf("native window presentation is not implemented: %s", action)
			}
		}
		for _, subscribe := range []func(func()) func(){win.OnFocus, win.OnBlur, win.OnShow, win.OnHide, win.OnMinimize, win.OnRestore, win.OnMaximize, win.OnUnmaximize} {
			subscribe(a.reportCoreWindowState)
		}
		var tray *mygo.Tray
		showWindow := func() { win.Show(); win.Restore(); win.Focus() }
		trayMenu := func() *mygo.Menu {
			tx := locales.In(mygo.App.Locale())
			return mygo.NewMenu([]*mygo.MenuItem{
				{Label: tx.OpenGitowl(), Click: func(*mygo.MenuItem, *mygo.Window) { showWindow() }},
				mygo.Separator(),
				{Label: tx.ExitApplication(), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
			})
		}
		a.prepareTray = func(enabled bool) error {
			if !enabled {
				if tray != nil {
					tray.Destroy()
					tray = nil
				}
				return nil
			}
			if tray != nil {
				return nil
			}
			icon, err := loadLogoPNG(resources)
			if err != nil {
				return err
			}
			tray, err = mygo.NewTray(mygo.TrayOptions{ID: "io.github.oubigfa.gitowl", Icon: icon, ToolTip: "Gitowl", Menu: trayMenu()})
			return err
		}
		if a.windowPreferences.CloseBehavior == closeWindowToTray {
			if err := a.prepareTray(true); err != nil {
				// A failed tray must never leave a hidden, unreachable application.
				a.windowPreferences.CloseBehavior, a.windowPreferenceErr = closeWindowExit, err
			}
		}
		win.OnClose(func(e *mygo.CloseEvent) { a.handleWindowClose(e, win.Hide) })
		mygo.App.OnBeforeQuit(a.handleBeforeQuit)
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
			if tray != nil {
				tray.Destroy()
				tray = nil
			}
		})
		win.OnClosed(a.close)
		menu := &nativeMenuHost{
			execute: func(id string) {
				if a.core == nil || a.closed {
					return
				}
				core, ctx := a.core, a.ctx
				a.queueCore(nativeOperation{
					call:     func() (backend.KernelResult, error) { return core.ExecuteMenu(ctx, id) },
					complete: func(_ backend.KernelResult, err error) { a.openErr = err },
				})
			},
			zoom: func(action string) {
				if action == "selectAll" {
					a.coreWindowAction("select-all")
					return
				}
				a.coreZoom = nextNativeMenuZoom(a.coreZoom, action)
				a.reportCoreWindowState()
				win.Invalidate()
			},
		}
		a.syncMenu = func() {
			state := backend.KernelMenuDTO{}
			if a.coreSnapshot != nil {
				state = a.coreSnapshot.Menu
			}
			menu.update(state, mygo.App.Locale(), a.core != nil && !a.closed && a.serviceErr == nil)
		}
		a.syncMenu()
		mygo.App.OnLocaleChanged(func(string) {
			win.SetTitle(locales.AppName())
			a.syncMenu()
			if tray != nil {
				tray.SetMenu(trayMenu())
			}
		})
		if a.serviceErr == nil {
			var err error
			core, err = backend.StartKernel(config, func(event backend.KernelEvent) { win.Update(func() { a.receiveCoreEvent(event) }) })
			a.core, a.serviceErr = core, err
			if core != nil {
				go func() { <-core.Done(); win.Update(func() { a.serviceStopped(core.Err()) }) }()
				a.connect("")
				// Initial paths use the same original CLI action as later launches;
				// do not replace its repository-root/clone/checkout decisions.
				initialLaunch.openPath = initialPath
				launches.start(initialLaunch)
				a.reportCoreWindowState()
			}
		}
		win.Invalidate()
	})
	// Catalogs follow system languages on first launch, and MyGo retains explicit
	// interface choices. Commit-generation language remains a separate gate.
	return mygo.App.Run()
}
