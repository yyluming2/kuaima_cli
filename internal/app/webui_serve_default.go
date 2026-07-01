//go:build !darwin || !cgo

package app

import (
	"errors"
	"net"
	"net/http"
)

func serveWebUI(server *http.Server, ln net.Listener, url string) error {
	stopTray := startWebUITray(url, func() {
		_ = server.Close()
	})
	if stopTray != nil {
		defer stopTray()
	}
	err := server.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
