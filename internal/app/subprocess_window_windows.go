//go:build windows

package app

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func hideSubprocessWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
