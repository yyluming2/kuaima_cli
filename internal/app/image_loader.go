package app

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

const remoteImageDownloadRetries = 3

// Some image hosts reject the default Go HTTP client user agent even though
// the same URL is accessible from a browser.
const remoteImageDownloadUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36"

func loadImageCandidate(ctx context.Context, httpClient *http.Client, candidate imageCandidate) ([]byte, string, error) {
	switch candidate.Kind {
	case "url":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate.Value, nil)
		if err != nil {
			return nil, "", err
		}
		downloadClient := httpClient
		var customClient *http.Client
		var lastErr error
		for attempt := 0; attempt <= remoteImageDownloadRetries; attempt++ {
			if attempt > 0 {
				delay := time.Duration(250*(1<<(attempt-1))) * time.Millisecond
				select {
				case <-ctx.Done():
					return nil, "", ctx.Err()
				case <-time.After(delay):
				}
			}
			resp, requestErr := downloadClient.Do(req)
			if requestErr == nil {
				data, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 && readErr == nil && len(data) > 0 {
					mediaType := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
					if mediaType == "" {
						mediaType = http.DetectContentType(data)
					}
					if attempt > 0 {
						log.Printf("remote image download recovered after retry attempt=%d url=%s", attempt+1, candidate.Value)
					}
					return data, mediaType, nil
				}
				if resp.StatusCode < 500 && resp.StatusCode >= 400 && readErr == nil && attempt > 0 {
					return nil, "", fmt.Errorf("download image failed: %s: %s", candidate.Value, resp.Status)
				}
				if readErr != nil {
					lastErr = readErr
				} else {
					lastErr = fmt.Errorf("download image failed: %s", resp.Status)
				}
			} else {
				lastErr = requestErr
			}
			if attempt == 0 {
				customClient = clientForRemoteImageDownload(httpClient)
				if customClient != nil {
					downloadClient = customClient
				}
			}
			if attempt < remoteImageDownloadRetries {
				log.Printf("remote image download failed, retrying attempt=%d/%d url=%s error=%v", attempt+1, remoteImageDownloadRetries, candidate.Value, lastErr)
			}
		}
		return nil, "", fmt.Errorf("download image failed after %d retries: %s: %w", remoteImageDownloadRetries, candidate.Value, lastErr)
	case "data":
		meta, data, ok := strings.Cut(candidate.Value, ",")
		if !ok {
			return nil, "", errors.New("invalid data URL image result")
		}
		mediaType := strings.TrimPrefix(strings.TrimSuffix(meta, ";base64"), "data:")
		bytes, err := base64.StdEncoding.DecodeString(cleanBase64(data))
		return bytes, mediaType, err
	default:
		bytes, err := base64.StdEncoding.DecodeString(cleanBase64(candidate.Value))
		if err != nil {
			return nil, "", fmt.Errorf("decode image: %w", err)
		}
		mediaType := candidate.MediaType
		if mediaType == "" {
			mediaType = http.DetectContentType(bytes)
		}
		return bytes, mediaType, nil
	}
}

// clientForRemoteImageDownload keeps the caller's timeout and transport
// settings while replacing the standard TLS ClientHello with uTLS's Chrome
// profile. The User-Agent alone does not change a request's TLS fingerprint.
func clientForRemoteImageDownload(httpClient *http.Client) *http.Client {
	if httpClient == nil {
		return httpClient
	}
	baseTransport := httpClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	transport, ok := baseTransport.(*http.Transport)
	if !ok {
		clientCopy := *httpClient
		clientCopy.Transport = remoteImageDownloadRoundTripper{rt: baseTransport}
		return &clientCopy
	}
	transport = transport.Clone()
	transport.DialTLSContext = chromeDialTLSContext(transport.DialContext, transport.TLSClientConfig)
	clientCopy := *httpClient
	clientCopy.Transport = remoteImageDownloadRoundTripper{rt: transport}
	return &clientCopy
}

type remoteImageDownloadRoundTripper struct {
	rt http.RoundTripper
}

func (t remoteImageDownloadRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", remoteImageDownloadUserAgent)
	return t.rt.RoundTrip(clone)
}

func chromeDialTLSContext(dialContext func(context.Context, string, string) (net.Conn, error), baseConfig *tls.Config) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if dialContext == nil {
			dialContext = (&net.Dialer{}).DialContext
		}
		rawConn, err := dialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		host, _, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			rawConn.Close()
			return nil, splitErr
		}
		// The standard Transport cannot consume an h2 connection when its TLS
		// handshake is supplied through DialTLSContext. Advertise HTTP/1.1 here
		// so a server cannot select h2 and feed binary frames to the HTTP/1.x
		// parser. The ClientHello still uses the Chrome uTLS profile.
		config := &utls.Config{ServerName: host, NextProtos: []string{"http/1.1"}}
		if baseConfig != nil {
			config.RootCAs = baseConfig.RootCAs
			config.InsecureSkipVerify = baseConfig.InsecureSkipVerify //nolint:gosec // preserve caller transport behavior
			config.ServerName = baseConfig.ServerName
			config.MinVersion = baseConfig.MinVersion
			config.MaxVersion = baseConfig.MaxVersion
			if baseConfig.NextProtos != nil {
				config.NextProtos = baseConfig.NextProtos
			}
		}
		if config.ServerName == "" {
			config.ServerName = host
		}
		// Chrome's preset includes h2 in ALPN. Build that preset explicitly and
		// replace only ALPN so net/http's HTTP/1.x transport remains compatible.
		spec, specErr := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
		if specErr != nil {
			rawConn.Close()
			return nil, specErr
		}
		for _, extension := range spec.Extensions {
			if alpn, ok := extension.(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"http/1.1"}
			}
		}
		config.NextProtos = []string{"http/1.1"}
		conn := utls.UClient(rawConn, config, utls.HelloCustom)
		if err := conn.ApplyPreset(&spec); err != nil {
			rawConn.Close()
			return nil, err
		}
		if err := conn.HandshakeContext(ctx); err != nil {
			rawConn.Close()
			return nil, err
		}
		return conn, nil
	}
}

func extensionForMediaType(mediaType, source string) string {
	if isHTTPURL(source) {
		if ext := filepath.Ext(pathFromURL(source)); ext != "" {
			return ext
		}
	}
	if mediaType != "" {
		if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
			return exts[0]
		}
	}
	return ".png"
}

func pathFromURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return parsed.Path
}
