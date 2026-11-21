package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/netproxy"
	"github.com/egoist/mygo/transfer"
)

// The business backend delegates OS work to the installed MyGo APIs. Arguments
// are decoded at this process boundary; failures return to the original caller.
func nativePlatformRequest(ctx context.Context, request backend.KernelPlatformRequest) (any, error) {
	text := func(index int) (string, error) {
		var value string
		if index >= len(request.Args) || json.Unmarshal(request.Args[index], &value) != nil {
			return "", errors.New("invalid native platform argument")
		}
		return value, nil
	}
	switch request.Operation {
	case "should-use-dark-colors":
		return mygo.Theme.IsDark(), nil
	case "set-native-theme-source":
		value, err := text(0)
		if err != nil {
			return nil, err
		}
		if value != "system" && value != "light" && value != "dark" {
			return nil, errors.New("invalid native theme")
		}
		mygo.Theme.SetSource(mygo.ThemeSource(value))
		return nil, nil
	case "get-path":
		name, err := text(0)
		if err != nil {
			return nil, err
		}
		return mygo.App.Path(mygo.PathName(name))
	case "beep":
		mygo.Shell.Beep()
		return nil, nil
	case "write-clipboard-text":
		value, err := text(0)
		if err != nil {
			return nil, err
		}
		return nil, mygo.Clipboard.Write(transfer.New(transfer.NewItem(transfer.Bytes(transfer.Text, []byte(value)))))
	case "move-to-trash":
		path, err := text(0)
		if err != nil {
			return nil, err
		}
		return nil, mygo.Shell.TrashItem(path)
	case "show-item-in-folder":
		path, err := text(0)
		if err != nil {
			return nil, err
		}
		mygo.Shell.ShowItemInFolder(path)
		return nil, nil
	case "open-path", "unsafe-open-directory":
		path, err := text(0)
		if err != nil {
			return nil, err
		}
		if err := mygo.Shell.OpenPath(path); err != nil {
			return nil, err
		}
		return "", nil
	case "resolve-proxy", "resolve-proxy-url":
		raw, err := text(0)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		proxy, err := netproxy.Proxy(req)
		if err != nil {
			return nil, err
		}
		if request.Operation == "resolve-proxy-url" {
			if proxy == nil {
				return nil, nil
			}
			return proxy.String(), nil
		}
		if proxy == nil {
			return "DIRECT", nil
		}
		switch proxy.Scheme {
		case "http":
			return "PROXY " + proxy.Host, nil
		case "https":
			return "HTTPS " + proxy.Host, nil
		case "socks5", "socks5h":
			return "SOCKS5 " + proxy.Host, nil
		default:
			return nil, fmt.Errorf("unsupported system proxy scheme: %s", proxy.Scheme)
		}
	case "get-notifications-permission":
		if mygo.NotificationsSupported() {
			return "granted", nil
		}
		return "denied", nil
	case "request-notifications-permission":
		return mygo.NotificationsSupported(), nil
	case "show-notification":
		title, err := text(0)
		if err != nil {
			return nil, err
		}
		body, err := text(1)
		if err != nil {
			return nil, err
		}
		id, err := text(3)
		if err != nil || id == "" {
			return nil, errors.New("missing persistent notification ID")
		}
		notification := mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body})
		if err := notification.ShowContext(ctx); err != nil {
			return nil, err
		}
		return notification.ID(), nil
	default:
		return nil, fmt.Errorf("unsupported native platform operation: %s", request.Operation)
	}
}
