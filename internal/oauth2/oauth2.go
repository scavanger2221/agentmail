package oauth2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// TokenData holds OAuth2 token information for storage.
type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"`
	TokenType    string `json:"token_type"`
}

// TokenPath returns the path for stored OAuth2 tokens for an account.
func TokenPath(accountName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "agentmail", "tokens", accountName+".json")
}

// LoadToken loads an OAuth2 token from disk.
func LoadToken(accountName string) (*oauth2.Token, error) {
	path := TokenPath(accountName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read token: %w", err)
	}

	var td TokenData
	if err := json.Unmarshal(data, &td); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	expiry, _ := time.Parse(time.RFC3339, td.Expiry)

	return &oauth2.Token{
		AccessToken:  td.AccessToken,
		RefreshToken: td.RefreshToken,
		Expiry:       expiry,
		TokenType:    td.TokenType,
	}, nil
}

// SaveToken saves an OAuth2 token to disk.
func SaveToken(accountName string, token *oauth2.Token) error {
	path := TokenPath(accountName)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}

	td := TokenData{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry.Format(time.RFC3339),
		TokenType:    token.TokenType,
	}

	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// GetGmailOAuthConfig returns an OAuth2 config for Gmail IMAP/SMTP.
func GetGmailOAuthConfig() *oauth2.Config {
	clientID := os.Getenv("AGENTMAIL_GMAIL_CLIENT_ID")
	clientSecret := os.Getenv("AGENTMAIL_GMAIL_CLIENT_SECRET")

	if clientID == "" || clientSecret == "" {
		return nil
	}

	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes: []string{
			"https://mail.google.com/",
		},
	}
}

// Authorize runs the OAuth2 flow and returns a token.
// Tries (in order): local-server browser flow → device flow → manual copy-paste.
func Authorize(cfg *oauth2.Config) (*oauth2.Token, error) {
	// 1. Try local server flow (best UX — browser opens, auto-capture)
	token, err := localServerFlow(cfg)
	if err == nil {
		return token, nil
	}

	// 2. Try device code flow
	token, err = deviceFlow(cfg)
	if err == nil {
		return token, nil
	}

	// 3. Fallback to manual copy-paste
	return manualFlow(cfg)
}

// localServerFlow starts a temp HTTP server, opens the browser, and captures the callback.
func localServerFlow(cfg *oauth2.Config) (*oauth2.Token, error) {
	ctx := context.Background()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// Build redirect URI
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// Create a clone of the config with the redirect URI
	localCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     cfg.Endpoint,
		RedirectURL:  redirectURI,
		Scopes:       cfg.Scopes,
	}

	authURL := localCfg.AuthCodeURL("state", oauth2.AccessTypeOffline)

	// Channel to receive the auth code
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	// HTTP server for the callback
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<h1>Authorization failed</h1><p>%s</p><p>You can close this window.</p>", errMsg)
			errCh <- fmt.Errorf("authorization failed: %s", errMsg)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>✓ Authorized!</h1><p>You can close this window.</p></body></html>"))
		codeCh <- code
	})

	server := &http.Server{Handler: mux}

	// Start server in background
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Open browser
	fmt.Fprintf(os.Stderr, "\n=== Gmail Authorization ===\n")
	fmt.Fprintf(os.Stderr, "Opening browser for login...\n")
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "If the browser doesn't open, visit:\n%s\n", authURL)
	}

	// Wait for code or error or timeout
	var authCode string
	select {
	case code := <-codeCh:
		authCode = code
	case err := <-errCh:
		server.Close()
		return nil, err
	case <-time.After(5 * time.Minute):
		server.Close()
		return nil, fmt.Errorf("timed out waiting for authorization")
	}

	// Shut down server
	server.Close()

	// Exchange code for token
	token, err := localCfg.Exchange(ctx, authCode)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ Authorized successfully\n\n")
	return token, nil
}

// deviceFlow performs the OAuth2 device code flow.
func deviceFlow(cfg *oauth2.Config) (*oauth2.Token, error) {
	ctx := context.Background()

	deviceCode, err := requestDeviceCode(cfg)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "\n=== Gmail Authorization ===\n")
	fmt.Fprintf(os.Stderr, "1. Open: %s\n", deviceCode.VerificationURL)
	fmt.Fprintf(os.Stderr, "2. Enter code: %s\n", deviceCode.UserCode)
	fmt.Fprintf(os.Stderr, "Waiting for authorization...\n")

	token, err := pollForToken(cfg, ctx, deviceCode)
	if err != nil {
		return nil, fmt.Errorf("device flow: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ Authorized successfully\n\n")
	return token, nil
}

type deviceAuthResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func requestDeviceCode(cfg *oauth2.Config) (*deviceAuthResponse, error) {
	data := url.Values{
		"client_id": {cfg.ClientID},
		"scope":     {strings.Join(cfg.Scopes, " ")},
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/device/code", data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed: %s", string(body))
	}

	var result deviceAuthResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func pollForToken(cfg *oauth2.Config, ctx context.Context, device *deviceAuthResponse) (*oauth2.Token, error) {
	interval := device.Interval
	if interval < 5 {
		interval = 5
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		token, err := exchangeDeviceCode(cfg, ctx, device.DeviceCode)
		if err == nil {
			return token, nil
		}

		if strings.Contains(err.Error(), "authorization_pending") {
			continue
		}
		if strings.Contains(err.Error(), "slow_down") {
			interval += 5
			continue
		}
		return nil, err
	}
}

func exchangeDeviceCode(cfg *oauth2.Config, ctx context.Context, deviceCode string) (*oauth2.Token, error) {
	data := url.Values{
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"device_code":   {deviceCode},
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
	}

	resp, err := http.PostForm(cfg.Endpoint.TokenURL, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var token oauth2.Token
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

// manualFlow is the fallback copy-paste flow.
func manualFlow(cfg *oauth2.Config) (*oauth2.Token, error) {
	ctx := context.Background()

	authURL := cfg.AuthCodeURL("state", oauth2.AccessTypeOffline)

	fmt.Fprintf(os.Stderr, "\n=== Gmail Authorization (manual) ===\n")
	fmt.Fprintf(os.Stderr, "Open this URL in your browser:\n\n%s\n\n", authURL)
	fmt.Fprintf(os.Stderr, "After authorizing, paste the authorization code here: ")

	var code string
	if _, err := fmt.Scanln(&code); err != nil {
		return nil, fmt.Errorf("read code: %w", err)
	}

	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	fmt.Fprintf(os.Stderr, "✓ Authorized successfully\n\n")
	return token, nil
}

// openBrowser opens the default browser to the given URL.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}
