package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func runAsk(args []string) error {
	cfg, err := loadAppConfig()
	if err != nil {
		return err
	}
	fs := newFlagSet("ask")
	opts := addResponseFlagsWithConfig(fs, cfg)
	imageGeneration := fs.Bool("image-generation", false, "启用图片生成工具")
	saveDir := addSaveImagesFlagWithConfig(fs, cfg, ".")
	inputOpts := addInputFlagsWithConfig(fs, cfg)
	imageOpts := addImageFlags(fs, cfg)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if err := imageOpts.validate(); err != nil {
		return err
	}
	if err := persistConfigFlags(fs, cfg); err != nil {
		return err
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		var err error
		prompt, err = readPromptFromStdin()
		if err != nil {
			return err
		}
	}
	if !inputOpts.hasPromptOrAttachments(prompt) {
		return errors.New("必须提供提示词或附件")
	}

	c, err := opts.newClient()
	if err != nil {
		return err
	}
	defer c.Close()

	input, err := inputOpts.buildInput(context.Background(), c, prompt, strings.TrimSpace(*opts.system))
	if err != nil {
		return err
	}
	req := responseRequest{
		Model:  *opts.model,
		Input:  input,
		Stream: *opts.stream,
	}
	if *imageGeneration {
		tool := imageOpts.responseTool()
		//指定模型
		addAutoString(tool, "model", opts.imageModel)
		//mask图片
		if imageOpts.mask != nil && *imageOpts.mask != "" {
			maskURL, err := imageInputURL(context.Background(), c, *imageOpts.mask, *inputOpts.fileFormat)
			if err != nil {
				return err
			}
			tool["input_image_mask"] = map[string]any{
				"image_url": maskURL,
			}
		}

		req.Tools = []map[string]any{tool}
		req.ToolChoice = map[string]any{"type": "image_generation"}

		req.Reasoning = defaultImageReasoning
	}

	runCount := 1
	if *imageGeneration {
		runCount = imageOpts.responseRunCount()
	}
	for i := 0; i < runCount; i++ {
		var resp *responsePayload
		imageSaver := newResponseImageSaver(context.Background(), c.httpClient, *saveDir, *imageOpts.upscale, stringValue(imageOpts.size))
		if *opts.stream {
			resp, err = c.createResponseStreamWithImages(context.Background(), req, os.Stdout, func(candidate imageCandidate) error {
				return saveStreamImage(imageSaver, candidate)
			})
			if err == nil {
				fmt.Fprintln(os.Stdout)
			}
		} else {
			resp, err = c.createResponse(context.Background(), req)
			if err == nil {
				text := strings.TrimSpace(resp.text())
				if text != "" {
					fmt.Println(text)
				}
			}
		}
		if err != nil {
			return err
		}
		if _, err := saveResponseImagesWithSaver(imageSaver, resp); err != nil {
			return err
		}
	}
	return nil
}

func readPromptFromStdin() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}

	fmt.Fprint(os.Stderr, "> ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "", scanner.Err()
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func runChat(args []string) error {
	cfg, err := loadAppConfig()
	if err != nil {
		return err
	}
	fs := newFlagSet("chat")
	opts := addResponseFlagsWithConfig(fs, cfg)
	inputOpts := addInputFlagsWithConfig(fs, cfg)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if err := persistConfigFlags(fs, cfg); err != nil {
		return err
	}

	c, err := opts.newClient()
	if err != nil {
		return err
	}
	defer c.Close()

	fmt.Println("简单对话模式。输入 /exit 或 /quit 退出。")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if text == "/exit" || text == "/quit" {
			break
		}
		input, err := inputOpts.buildInput(context.Background(), c, text, strings.TrimSpace(*opts.system))
		if err != nil {
			return err
		}
		req := responseRequest{Model: *opts.model, Input: input, Stream: *opts.stream}
		if *opts.stream {
			if _, err := c.createResponseStream(context.Background(), req, os.Stdout); err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout)
			continue
		}
		resp, err := c.createResponse(context.Background(), req)
		if err != nil {
			return err
		}
		answer := strings.TrimSpace(resp.text())
		if answer == "" {
			answer = "(空响应)"
		}
		fmt.Println(answer)
	}
	return scanner.Err()
}
