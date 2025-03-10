package oidc

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gofrs/uuid"
	"github.com/h44z/wg-portal/internal/oauth/oauthproviders"
	"github.com/h44z/wg-portal/internal/oauth/userprofile"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

type oidcProvider struct {
	oauth2.Config
	oidcProvider *oidc.Provider
	id           string
	createUsers  bool
	verifyEmail  bool
}

type ProviderConfig struct {
	DiscoveryURL string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	CreateUsers  bool
	VerifyEmail  bool
}

func New(c ProviderConfig) (oauthproviders.Provider, error) {
	provider, err := oidc.NewProvider(context.Background(), c.DiscoveryURL)
	if err != nil {
		return nil, err
	}

	config := oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		RedirectURL:  c.RedirectURL,
	}

	return &oidcProvider{
		Config:       config,
		oidcProvider: provider,
		id:           uuid.NewV3(uuid.NamespaceURL, c.DiscoveryURL).String(),
		createUsers:  c.CreateUsers,
		verifyEmail:  c.VerifyEmail,
	}, nil
}

func (p oidcProvider) ID() string {
	return p.id
}

func (p oidcProvider) UserInfo(ctx context.Context, ts oauth2.TokenSource) (userprofile.Profile, error) {
	t, err := ts.Token()
	if err != nil {
		return userprofile.Profile{}, err
	}

	rawIDToken, ok := t.Extra("id_token").(string)
	if !ok {
		return userprofile.Profile{}, fmt.Errorf("oidc: missing id_token")
	}

	verifier := p.oidcProvider.Verifier(&oidc.Config{ClientID: p.Config.ClientID})

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return userprofile.Profile{}, err
	}

	// Extract custom claims
	var claims struct {
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		GivenName     string `json:"given_name"`
		FamilyName    string `json:"family_name"`
		Email         string `json:"email"`
	}

	if err := idToken.Claims(&claims); err != nil {
		return userprofile.Profile{}, errors.WithMessage(err, "oidc: cannot parte the token claims")
	}

	if p.verifyEmail && !claims.EmailVerified {
		return userprofile.Profile{}, errors.New("oidc: the user has an unverified email address")
	}

	return userprofile.Profile{
		FirstName: claims.GivenName,
		LastName:  claims.FamilyName,
		Email:     claims.Email,
	}, nil
}

func (p oidcProvider) CanCreateUsers() bool {
	return p.createUsers
}
