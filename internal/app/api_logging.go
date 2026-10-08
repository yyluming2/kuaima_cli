package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const maxLoggedLongString = 256

func (c *client) logRequest(req *http.Request, body []byte) {
	if !logEnabled(c.verbose, c.logWriter) {
		return
	}
	w := logOutput(c.verbose, c.logWriter)
	fmt.Fprintf(w, "\n%s --- request ---\n%s %s\n", logEntryPrefix(3), req.Method, req.URL.String())
	if req.Header.Get("Authorization") != "" {
		fmt.Fprintln(w, "Authorization: Bearer <redacted>")
	}
	if contentType := req.Header.Get("Content-Type"); contentType != "" {
		fmt.Fprintf(w, "Content-Type: %s\n", contentType)
	}
	if accept := req.Header.Get("Accept"); accept != "" {
		fmt.Fprintf(w, "Accept: %s\n", accept)
	}
	if len(body) > 0 {
		fmt.Fprintln(w, prettyJSON(body))
	}
}

func (c *client) logImageEditMultipart(req imageEditRequest) {
	if !logEnabled(c.verbose, c.logWriter) {
		return
	}
	w := logOutput(c.verbose, c.logWriter)
	f := make([]string, 0, len(req.Images))
	for _, image := range req.Images {
		f = append(f, fmt.Sprintf("source=%s", image.ImageURL[:logMinInt(len(image.ImageURL), 80)]))
	}
	fmt.Fprintf(w, "multipart fields: model=%q prompt=%q n=%d size=%q images=[%s]\n", req.Model, req.Prompt, req.N, req.Size, strings.Join(f, ", "))
}

func logMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *client) logTiming(started, first time.Time) {
	if !logEnabled(c.verbose, c.logWriter) {
		return
	}
	w := logOutput(c.verbose, c.logWriter)
	fmt.Fprintf(w, "\n%s --- timing ---\n", logEntryPrefix(3))
	if first.IsZero() {
		fmt.Fprintln(w, "首字时间: n/a")
	} else {
		fmt.Fprintf(w, "首字时间: %s\n", first.Sub(started).Round(time.Millisecond))
	}
	fmt.Fprintf(w, "完整时间: %s\n", time.Since(started).Round(time.Millisecond))
}

func (c *client) logResponse(resp *http.Response, body []byte) {
	if !logEnabled(c.verbose, c.logWriter) {
		return
	}
	c.logResponseStatusWithCaller(resp, logCaller(2))
	c.logResponseBody(body)
}

func (c *client) logResponseStatus(resp *http.Response) {
	c.logResponseStatusWithCaller(resp, logCaller(2))
}

func (c *client) logResponseStatusWithCaller(resp *http.Response, caller string) {
	if !logEnabled(c.verbose, c.logWriter) {
		return
	}
	fmt.Fprintf(logOutput(c.verbose, c.logWriter), "\n%s --- response ---\n%s\n", logEntryPrefixForCaller(caller), resp.Status)
}

func (c *client) logResponseBody(body []byte) {
	if !logEnabled(c.verbose, c.logWriter) {
		return
	}
	w := logOutput(c.verbose, c.logWriter)
	if len(body) == 0 {
		fmt.Fprintln(w, "<empty>")
		return
	}
	fmt.Fprintln(w, prettyJSON(body))
}

func logEnabled(verbose bool, writer io.Writer) bool {
	return verbose || writer != nil
}

func logOutput(verbose bool, writer io.Writer) io.Writer {
	if verbose && writer != nil {
		return io.MultiWriter(os.Stderr, writer)
	}
	if writer != nil {
		return writer
	}
	if verbose {
		return os.Stderr
	}
	return io.Discard
}

func logEntryPrefix(skip int) string {
	return logEntryPrefixForCaller(logCaller(skip))
}

func logEntryPrefixForCaller(caller string) string {
	return fmt.Sprintf("[%s] [%s]", time.Now().Format("2006-01-02 15:04:05.000"), caller)
}

func logCaller(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}
	name := fn.Name()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func prettyJSON(data []byte) string {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return prettyEventStream(data)
	}
	value = truncateLogValue(value, "")
	formatted, err := marshalLogJSON(value, true)
	if err != nil {
		return string(data)
	}
	return string(formatted)
}

func prettyEventStream(data []byte) string {
	text := string(data)
	lines := strings.Split(text, "\n")
	changed := false
	for i, line := range lines {
		prefixLen := len(line) - len(strings.TrimLeft(line, " \t"))
		prefix := line[:prefixLen]
		rest := strings.TrimSpace(line[prefixLen:])
		if !strings.HasPrefix(rest, "data:") {
			continue
		}
		eventData := strings.TrimSpace(strings.TrimPrefix(rest, "data:"))
		if eventData == "" || eventData == "[DONE]" {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(eventData), &value); err != nil {
			continue
		}
		value = truncateLogValue(value, "")
		formatted, err := marshalLogJSON(value, false)
		if err != nil {
			continue
		}
		lines[i] = prefix + "data: " + string(formatted)
		changed = true
	}
	if !changed {
		return text
	}
	return strings.Join(lines, "\n")
}

func truncateLogValue(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		for childKey, child := range v {
			v[childKey] = truncateLogValue(child, childKey)
		}
		return v
	case []any:
		for i, child := range v {
			v[i] = truncateLogValue(child, key)
		}
		return v
	case string:
		if shouldTruncateLogString(key, v) {
			return truncateLoggedString(v)
		}
		return v
	}
	return value
}

func shouldTruncateLogString(key, value string) bool {
	if len(value) <= maxLoggedLongString {
		return false
	}
	lowerKey := strings.ToLower(key)
	if strings.Contains(lowerKey, "base64") ||
		strings.Contains(lowerKey, "b64") ||
		strings.Contains(lowerKey, "image") ||
		strings.Contains(lowerKey, "result") ||
		strings.Contains(lowerKey, "url") ||
		strings.HasPrefix(value, "data:image/") ||
		looksLikeLoggedBase64(value) {
		return true
	}
	return false
}

func looksLikeLoggedBase64(value string) bool {
	if len(value) < maxLoggedLongString {
		return false
	}
	sample := value
	if len(sample) > 512 {
		sample = sample[:512]
	}
	var valid int
	for _, r := range sample {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '/', r == '=', r == '\r', r == '\n':
			valid++
		default:
			return false
		}
	}
	return valid == len(sample)
}

func truncateLoggedString(value string) string {
	return value[:maxLoggedLongString] + fmt.Sprintf("...<truncated %d chars>", len(value)-maxLoggedLongString)
}

func marshalLogJSON(value any, indent bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
