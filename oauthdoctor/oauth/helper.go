// Copyright 2019 Google LLC
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package oauth implements functions to diagnose the supported OAuth2 flows
// (web and installed app flows) in a Google Ads API client library client
// environment.
package oauth

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"oauthdoctor/diag"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// This is a list of error codes (not comprehensive) returned by Google OAuth2
// endpoint based on Google Ads API scope.
const (
	AccessNotPermittedForManagerAccount = iota
	GoogleAdsAPIDisabled
	InvalidClientInfo
	InvalidRefreshToken
	InvalidCustomerID
	MissingDevToken
	Unauthenticated
	Unauthorized
	UnknownError
)

const (
	// Web is the constant that identifies the oauth path.
	Web string = "web"
	// InstalledApp is the constant that identifies the installed application oauth path.
	InstalledApp string = "installed_app"
)

// Config is a required configuration for diagnosing the OAuth2 flow based on
// the client library configuration.
type Config struct {
	ConfigFile diag.ConfigFile
	CustomerID string
	OAuthType  string
	Verbose    bool
}

// SimulateOAuthFlow simulates the OAuth2 flows supported by the Google Ads API
// client libraries.
func (c *Config) SimulateOAuthFlow() {
	switch c.OAuthType {
	case Web:
		c.simulateWebFlow()
	case InstalledApp:
		c.simulateAppFlow()
	}
}

// decodeError checks the JSON response in the error and determines the error
// code.
func (c *Config) decodeError(err error) int32 {
	errstr := err.Error()

	if strings.Contains(errstr, "invalid_client") {
		// Client ID and/or secret is invalid
		return InvalidClientInfo
	}
	if strings.Contains(errstr, "unauthorized_client") {
		// The given refresh token may not be generated with the given client ID
		// and secret
		return Unauthorized
	}
	if strings.Contains(errstr, "invalid_grant") {
		// Refresh token is not valid for any users
		return InvalidRefreshToken
	}
	if strings.Contains(errstr, "refresh token is not set") {
		return InvalidRefreshToken
	}
	if strings.Contains(errstr, "USER_PERMISSION_DENIED") {
		// User doesn't have permission to access Google Ads account
		return InvalidRefreshToken
	}
	if strings.Contains(errstr, "\"PERMISSION_DENIED\"") {
		return GoogleAdsAPIDisabled
	}
	if strings.Contains(errstr, "UNAUTHENTICATED") {
		return Unauthenticated
	}
	if strings.Contains(errstr, "CANNOT_BE_EXECUTED_BY_MANAGER_ACCOUNT") {
		// Request cannot be executed by a manager account
		return AccessNotPermittedForManagerAccount
	}
	if strings.Contains(errstr, "DEVELOPER_TOKEN_PARAMETER_MISSING") {
		return MissingDevToken
	}
	if strings.Contains(errstr, "INVALID_CUSTOMER_ID") {
		return InvalidCustomerID
	}
	return UnknownError
}

// diagnose handles the error by guiding the user to take appropriate
// actions to fix the OAuth2 error based on the error code.
func (c *Config) diagnose(err error) {
	// Print the given message from JSON response if there's any
	var parsedMsg map[string]interface{}
	if err := json.Unmarshal([]byte(err.Error()), &parsedMsg); err == nil {
		errMsg := parsedMsg["error"].(map[string]interface{})["message"]
		log.Print("JSON response error: " + errMsg.(string))
	}

	switch c.decodeError(err) {
	case AccessNotPermittedForManagerAccount:
		log.Print("ERROR: Your credentials are not sufficient to access to a " +
			"manager account.\nPlease login with a Google Ads account with manager access.")
	case GoogleAdsAPIDisabled:
		log.Print("Press <Enter> to continue after you enable Google Ads API")
		reader := bufio.NewReader(os.Stdin)
		reader.ReadString('\n')
	case InvalidClientInfo:
		log.Print("ERROR: Your client ID and/or secret may be invalid.")
		replaceCloudCredentials(c.ConfigFile)
	case InvalidRefreshToken, Unauthorized:
		log.Print("ERROR: Your refresh token may be invalid.")
	case MissingDevToken:
		log.Print("ERROR: Your developer token is missing in the configuration file")
		replaceDevToken(c.ConfigFile)
	case Unauthenticated:
		log.Print("ERROR: The login email may not have access to the given account.")
	case InvalidCustomerID:
		log.Print("ERROR: You customer ID is invalid.")
	default:
		log.Print("ERROR: Your credentials are invalid but we cannot determine " +
			"the exact error. Please verify your developer token, client ID, " +
			"client secret and refresh token.")
	}
}

// replaceCloudCredentials prompts the user to create a new client ID and
// secret and to then enter them at the prompt. The values entered will
// replace the existing values in the client library configuration file.
func replaceCloudCredentials(c diag.ConfigFile) {
	log.Print("Follow this guide to setup your OAuth2 client ID " +
		"and client secret: " +
		"https://developers.google.com/adwords/api/docs/guides/first-api-call#set_up_oauth2_authentication")
	fmt.Print("New Client ID >> ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')

	strings.Replace(input, "\n", "", -1)
	clientID := strings.Replace(input, "\n", "", -1)

	fmt.Print("New Client Secret >> ")
	reader = bufio.NewReader(os.Stdin)
	input, _ = reader.ReadString('\n')

	clientSecret := strings.Replace(input, "\n", "", -1)
	c.ReplaceConfig(diag.ClientID, clientID)
	c.ReplaceConfig(diag.ClientSecret, clientSecret)
}

// replaceDevToken guides the user to retrieve their developer token and
// enter it at the prompt. The entered value will replace the existing
// developer token in the client library configuration file.
func replaceDevToken(c diag.ConfigFile) {
	log.Print("Please follow this guide to retrieve your developer token: " +
		"https://developers.google.com/adwords/api/docs/guides/signup#step-2")
	log.Print("Pleae enter a new Developer Token here and it will replace " +
		"the one in your client library configuration file")
	fmt.Print("New Developer Token >> ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')

	strings.Replace(input, "\n", "", -1)
	devToken := strings.Replace(input, "\n", "", -1)
	c.ReplaceConfig(diag.DevToken, devToken)
}

// replaceRefreshToken asks the user if they want to replace the refresh
// token in the configuration file with the newly generated value.
func replaceRefreshToken(c diag.ConfigFile, refreshToken string) {
	log.Print("Would you like to replace your refresh token in the " +
		"client library config file with the new one generated?")
	fmt.Print("Enter Y for Yes [Anything else is No] >> ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')

	answer = strings.Replace(answer, "\n", "", -1)

	if answer == "Y" {
		c.ReplaceConfig(diag.RefreshToken, refreshToken)
	} else {
		log.Print("Refresh token is NOT replaced")
	}
}

// oauth2Conf creates a corresponding OAuth2 config struct based on the
// given configuration details. This is only applicable when a refresh token
// is not given.
func (c *Config) oauth2Conf(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ConfigFile.ClientID,
		ClientSecret: c.ConfigFile.ClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"https://www.googleapis.com/auth/adwords"},
		Endpoint:     google.Endpoint,
	}
}

// Given the auth code returned after the authentication and authorization
// step, oauth2Client creates a HTTP client with an authorized access token.
func (c *Config) oauth2Client(code string) (*http.Client, string) {
	conf := c.oauth2Conf(InstalledAppRedirectURL)
	// Handle the exchange code to initiate a transport.
	token, err := conf.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Fatal(err)
	}
	return conf.Client(oauth2.NoContext, token), token.RefreshToken
}

// getAccount makes a HTTP request to Google Ads API customer account
// endpoint and parse the JSON response.
func (c *Config) getAccount(client *http.Client) (*bytes.Buffer, error) {
	req, _ := http.NewRequest("GET",
		"https://googleads.googleapis.com/v1/customers/"+c.CustomerID,
		nil)
	req.Header.Set("developer-token", c.ConfigFile.DevToken)
	if c.ConfigFile.LoginCustomerID != "" {
		req.Header.Set("login-customer-id", c.ConfigFile.LoginCustomerID)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)

	var jsonBody map[string]interface{}
	json.Unmarshal(buf.Bytes(), &jsonBody)

	if jsonBody["error"] != nil {
		return nil, errors.New(buf.String())
	}

	return buf, nil
}

// ReadCustomerID retrieves the CID from stdin
func ReadCustomerID() string {
	reader := bufio.NewReader(os.Stdin)

	for {
		log.Print("Please enter a Google Ads account ID:")
		customerID, _ := reader.ReadString('\n')
		customerID = strings.TrimSpace(strings.Replace(customerID, "\n", "", -1))
		if customerID != "" {
			return strings.Replace(customerID, "-", "", -1)
		}
	}
}
