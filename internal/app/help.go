package app

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		commandUsage(fs.Output(), name, fs)
	}
	return fs
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kuaima_cli -version")
	fmt.Fprintln(w, "  kuaima_cli [ask] [options] prompt")
	fmt.Fprintln(w, "  kuaima_cli [ask] [options] < prompt.txt")
	fmt.Fprintln(w, "  kuaima_cli chat [options]")
	fmt.Fprintln(w, "  kuaima_cli image [options] prompt")
	fmt.Fprintln(w, "  kuaima_cli webui [options]")
	fmt.Fprintln(w, "  kuaima_cli balance [options]")
	fmt.Fprintln(w, "  kuaima_cli recharge [options]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  version  print the program version")
	fmt.Fprintln(w, "  ask      send one prompt and print the response; default command when omitted")
	fmt.Fprintln(w, "  chat     enter interactive chat mode")
	fmt.Fprintln(w, "  image    generate/edit images and save returned images")
	fmt.Fprintln(w, "  webui    start a local Web UI for image generation, balance, and recharge")
	fmt.Fprintln(w, "  balance  query current API key balance")
	fmt.Fprintln(w, "  recharge open recharge page or create a payment")
	fmt.Fprintln(w, "  help     print this help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Common options:")
	fmt.Fprintln(w, "  -file PATH_OR_URL       input text/image path or image URL; can repeat")
	fmt.Fprintln(w, "  -file-format FORMAT     local image input format: base64 or url; default base64")
	fmt.Fprintln(w, "  -image-generation       enable response image generation tool")
	fmt.Fprintln(w, "  -stream                 stream text/image events")
	fmt.Fprintln(w, "  -save-images DIR        save images from responses; empty disables saving")
	fmt.Fprintln(w, "  -model MODEL            default uses KUAIMA_MODEL or gpt-5.4-mini")
	fmt.Fprintln(w, "  -image-model MODEL      image generation model; default gpt-image-2")
	fmt.Fprintln(w, "  -image-size SIZE        auto, 1024x1024, 1536x1024, 1024x1536, or custom WxH")
	fmt.Fprintln(w, "  -image-quality QUALITY  auto, low, medium, high")
	fmt.Fprintln(w, "  -image-count N          number of images for image command")
	fmt.Fprintln(w, "  -image-output-format F  png, jpeg, or webp")
	fmt.Fprintln(w, "  -image-output-compression N  jpeg/webp compression, 0-100")
	fmt.Fprintln(w, "  -image-background BG    auto, transparent, or opaque")
	fmt.Fprintln(w, "  -image-moderation MODE  auto or low")
	fmt.Fprintln(w, "  -image-action ACTION    Responses image action: auto, generate, or edit")
	fmt.Fprintln(w, "  -image-upscale          enlarge saved images to the target size when smaller")
	fmt.Fprintln(w, "  -image-mask PATH_OR_URL Images Edit mask, used with image -file ...")
	fmt.Fprintln(w, "  -base-url URL           default https://img.szkmjb.com")
	fmt.Fprintln(w, "  -system TEXT            optional system/developer instruction")
	fmt.Fprintln(w, "  -api-key KEY            API key; overrides KUAIMA_API_KEY/OPENAI_API_KEY")
	fmt.Fprintln(w, "  -username USER          kuaima account; auto-generated and saved if omitted")
	fmt.Fprintln(w, "  -password PASS          kuaima password; auto-generated and saved if omitted")
	fmt.Fprintln(w, "  -log FILE               write request/response/timing logs")
	fmt.Fprintln(w, "  -v                      print debug logs to stderr")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Environment:")
	fmt.Fprintln(w, "  KUAIMA_API_KEY          API key; OPENAI_API_KEY fallback")
	fmt.Fprintln(w, "  KUAIMA_BASE_URL         default https://img.szkmjb.com")
	fmt.Fprintln(w, "  KUAIMA_OSS_URL          default https://oss.szkmjb.com")
	fmt.Fprintln(w, "  KUAIMA_MODEL            default gpt-5.4-mini")
	fmt.Fprintln(w, "  KUAIMA_IMAGE_MODEL      image generation model")
}

func commandUsage(w io.Writer, name string, fs *flag.FlagSet) {
	switch name {
	case "version":
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  kuaima_cli -version")
	case "ask":
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  kuaima_cli [ask] [options] prompt")
		fmt.Fprintln(w, "  kuaima_cli [ask] [options] < prompt.txt")
	case "chat":
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  kuaima_cli chat [options]")
	case "image":
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  kuaima_cli image [options] prompt")
	case "webui":
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  kuaima_cli webui [options]")
	case "login":
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  kuaima_cli login [options]")
	case "balance":
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  kuaima_cli balance [options]")
	case "recharge":
		fmt.Fprintln(w, "Usage:")
		fmt.Fprintln(w, "  kuaima_cli recharge [options]")
	default:
		fmt.Fprintf(w, "Usage:\n  kuaima_cli %s [options]\n", name)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fs.PrintDefaults()
}
