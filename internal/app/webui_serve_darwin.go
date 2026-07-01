//go:build darwin && cgo

package app

import (
	"errors"
	"net"
	"net/http"
)

func serveWebUI(server *http.Server, ln net.Listener, url string) error {
	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	startWebUITray(url, func() {
		_ = server.Close()
	})
	_ = server.Close()
	return <-errCh
}
