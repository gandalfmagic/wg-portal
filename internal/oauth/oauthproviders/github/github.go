package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/h44z/wg-portal/internal/oauth/oauthproviders"
	"github.com/h44z/wg-portal/internal/oauth/userprofile"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

const ProviderGithub oauthproviders.ProviderType = "github"

const (
	githubApiUserProfile = "https://api.github.com/user"
	githubApiUserEmails  = "https://api.github.com/user/emails"
)

type emails struct {
	Email      string `json:"email"`
	Primary    bool   `json:"primary"`
	Verified   bool   `json:"verified"`
	Visibility string `json:"visibility"`
}

type userInfo struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type provider struct {
	oauth2.Config
	id          string
	createUsers bool
}

func New(pc oauthproviders.ProviderConfig) oauthproviders.Provider {
	config := oauth2.Config{
		ClientID:     pc.ClientID,
		ClientSecret: pc.ClientSecret,
		Endpoint:     github.Endpoint,
		Scopes:       []string{"read:user", "user:email"},
		RedirectURL:  pc.RedirectURL,
	}

	return &provider{
		Config:      config,
		id:          string(ProviderGithub),
		createUsers: pc.CreateUsers,
	}
}

func (g provider) ID() string {
	return g.id
}

func (g provider) UserInfo(ctx context.Context, ts oauth2.TokenSource) (userprofile.Profile, error) {
	resp, err := oauthproviders.DoRequest(ctx, ts, githubApiUserProfile)
	if err != nil {
		return userprofile.Profile{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return userprofile.Profile{}, errors.New(fmt.Sprintf("github: returned status code %s", resp.Status))
	}

	var p userInfo
	if err = json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return userprofile.Profile{}, errors.WithMessage(err, "github: invalid response from the authentication sever")
	}

	if p.Email == "" {
		email, err := g.userEmail(ctx, ts)
		if err != nil {
			return userprofile.Profile{}, fmt.Errorf("github: user %s: %v", p.Name, err)
		}

		p.Email = email
	}

	if p.Name == "" {
		p.Name = p.Email
	}

	return userprofile.Profile{
		FirstName: p.Name,
		Email:     p.Email,
	}, nil
}

func (g provider) userEmail(ctx context.Context, ts oauth2.TokenSource) (string, error) {
	resp, err := oauthproviders.DoRequest(ctx, ts, githubApiUserEmails)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var emailList []emails
	if err = json.NewDecoder(resp.Body).Decode(&emailList); err != nil {
		return "", errors.WithMessage(err, "github: invalid response from the authentication sever")
	}

	for _, item := range emailList {
		if item.Primary && item.Verified {
			return item.Email, nil
		}
	}

	return "", errors.WithMessage(err, "github: no valid email found")
}

func (g provider) CanCreateUsers() bool {
	return g.createUsers
}
