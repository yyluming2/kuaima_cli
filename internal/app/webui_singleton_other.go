//go:build !windows

package app

import (
	"fmt"
	"net"
	"strings"
	"time"
)

func acquireWebUIInstance() (func(), bool, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:8789")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "address already in use") {
			// 端口被占用，检查是否有真实进程在监听
			conn, err := net.DialTimeout("tcp", "127.0.0.1:8789", 500*time.Millisecond)
			if err == nil {
				// 有连接成功，说明真的有服务在监听
				conn.Close()
				return func() {}, false, nil
			}
			// 连接失败，可能是 TIME_WAIT 状态，尝试强制监听
			return func() {}, true, nil
		}
		return nil, false, err
	}
	return func() { _ = ln.Close() }, true, nil
}

func notifyWebUIAlreadyRunning(message string) {
	fmt.Println(message)
}
