//go:build !windows

package app

import "os/exec"

func hideSubprocessWindow(_ *exec.Cmd) {}
