package app

import (
	"fmt"
	"os"
)

const (
	defaultBaseURL    = "https://img.szkmjb.com"
	defaultModel      = "gpt-5.6-luna"
	defaultImageModel = "gpt-image-2"

	defaultOssURL = "https://oss.szkmjb.com"
)

func Run(args []string) error {
	if len(args) == 0 && launchedFromFileExplorer() {
		return runWebUIAuto(nil)
	}

	if len(args) > 0 {
		switch args[0] {
		case "ask":
			return runAsk(args[1:])
		case "chat":
			return runChat(args[1:])
		case "image":
			return runImage(args[1:])
		case "webui":
			return runWebUI(args[1:])
		//case "login":
		//	return runLogin(args[1:])
		case "balance":
			return runBalance(args[1:])
		case "recharge":
			return runRecharge(args[1:])
		case "help", "-h", "--help":
			usage(os.Stdout)
			return nil
		case "-version", "--version", "version":
			fmt.Fprintln(os.Stdout, AppVersion)
			return nil
		}
	}

	if len(args) > 0 {
		return runAsk(args)
	}
	usage(os.Stdout)
	return nil
}
