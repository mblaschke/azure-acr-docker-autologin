package main

import (
	"fmt"
	"strings"
	"net/url"
	"net/http"
	"io/ioutil"
	"encoding/json"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/dgrijalva/jwt-go"
)

type azureAcr struct {
	loginServer string
	azureTenantId string
	servicePrincipalToken *adal.ServicePrincipalToken

}

type acrTokenPayload struct {
	Expiration int64  `json:"exp"`
	TenantID   string `json:"tenant"`
	Credential string `json:"credential"`
}

var (
	client = http.Client{}
)

func (a *azureAcr) GetName() string {
	return a.loginServer
}


func (a *azureAcr) FetchAcrToken() (refreshToken string, err error) {
	acrAuthEndpoint := fmt.Sprintf("https://%s/oauth2/exchange", a.loginServer)

	v := url.Values{}
	v.Set("grant_type", "access_token")
	v.Set("service", a.loginServer)
	v.Set("tenant", a.azureTenantId)
	v.Set("access_token", a.servicePrincipalToken.AccessToken)

	s := v.Encode()
	body := ioutil.NopCloser(strings.NewReader(s))
	req, err := http.NewRequest(http.MethodPost, acrAuthEndpoint, body)
	if err != nil {
		return "", err
	}

	req.ContentLength = int64(len(s))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)

	defer resp.Body.Close()
	responseBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return "", err
	}

	acrToken := struct {
		RefreshToken string `json:"refresh_token"`
	}{}

	if err := json.Unmarshal(responseBody, &acrToken); err != nil {
		return "", err
	}

	refreshToken = acrToken.RefreshToken
	return
}

func parseAcrToken(identityToken string) (token *acrTokenPayload, err error) {
	tokenSegments := strings.Split(identityToken, ".")
	if len(tokenSegments) < 2 {
		return nil, fmt.Errorf("Invalid existing refresh token length: %d", len(tokenSegments))
	}
	payloadSegmentEncoded := tokenSegments[1]
	var payloadBytes []byte
	if payloadBytes, err = jwt.DecodeSegment(payloadSegmentEncoded); err != nil {
		return nil, fmt.Errorf("Error decoding payload segment from refresh token, error: %s", err)
	}
	var payload acrTokenPayload
	if err = json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("Error unmarshalling acr payload, error: %s", err)
	}
	return &payload, nil
}
