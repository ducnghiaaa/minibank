package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// UserInfo reads the customer's profile from the OIDC userInfo endpoint (Cognito: /oauth2/userInfo).
// Cognito access tokens carry no name; with scope "profile" this endpoint returns it.
type UserInfo struct {
	URL    string
	Client *http.Client
}

// Name returns the "name" attribute of the user the access token belongs to.
func (u UserInfo) Name(ctx context.Context, accessToken string) (string, error) {
	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("userInfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userInfo: status %d", resp.StatusCode)
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("userInfo: %w", err)
	}
	return body.Name, nil
}
