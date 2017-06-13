/*
Copyright 2017 D2L Corporation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mitchellh/go-homedir"
)

type Config struct {
	AwsMount   string
	Role       string
	VaultUrl   *url.URL
	TokenPath  string
	NoncePath  string
	Agent      bool
	RetryDelay int
}

type SealStatus struct {
	Sealed      bool   `json:"sealed"`
	Version     string `json:"version"`
	ClusterName string `json:"cluster_name"`
}

type LoginRequest struct {
	Role  string `json:"role"`
	Pkcs7 string `json:"pkcs7"`
}

type ReLoginRequest struct {
	Role  string `json:"role"`
	Pkcs7 string `json:"pkcs7"`
	Nonce string `json:"nonce"`
}

type LoginResponse struct {
	Auth struct {
		Renewable     bool  `json:"renewable"`
		LeaseDuration int32 `json:"lease_duration"`
		MetaData      struct {
			RoleTagMaxTtl string `json:"role_tag_max_ttl"`
			Role          string `json:"role"`
			Region        string `json:"region"`
			Nonce         string `json:"nonce"`
			InstanceId    string `json:"instance_id"`
			AmiId         string `json:"ami_id"`
		} `json:"metadata"`
		Policies    []string `json:"policies"`
		Accessor    string   `json:"accessor"`
		ClientToken string   `json:"client_token"`
	} `json:"auth"`
	Warnings []string `json:"warnings"`
	WrapInfo struct {
		TTL             time.Duration `json:"ttl"`
		Token           string        `json:"token"`
		CreationTime    time.Time     `json:"creation_time"`
		WrappedAccessor string        `json:"wrapped_accessor"`
		Format          string        `json:"format"`
	} `json:"wrap_info"`
	LeaseDuration int32  `json:"lease_duration"`
	Renewable     bool   `json:"renewable"`
	LeaseId       string `json:"lease_id"`
	RequestId     string `json:"request_id"`
}

var client http.Client
var config Config

func init() {
	var err error
	var vaultUrlParameter string

	homeDir, err := homedir.Dir()
	check(err)

	flag.StringVar(&vaultUrlParameter, "vault-url", "https://vault.service.consul:8200", "the full url to the vault node to auth against")
	flag.StringVar(&config.Role, "role", "", "the vault role to request")
	flag.StringVar(&config.AwsMount, "aws-mount", "aws-ec2", "the AWS mount path (default: aws-ec2)")
	flag.StringVar(&config.NoncePath, "nonce-path", filepath.Join(homeDir, ".vault-nonce"), "the path to the nonce file")
	flag.StringVar(&config.TokenPath, "token-path", filepath.Join(homeDir, ".vault-token"), "the path to the token file")
	flag.BoolVar(&config.Agent, "agent", false, "setting this flag will run in agent mode")
	flag.IntVar(&config.RetryDelay, "retry-delay", 30, "The number of seconds between retries between failed login attempts")
	flag.Parse()

	if config.Role == "" {
		flag.Usage()
		log.Fatal("Must provide a role to auth with.")
	}

	config.VaultUrl, err = url.Parse(vaultUrlParameter)
	check(err)

	init_httpClient()
}

func main() {
	lease_renewal_time := time.Now()

	for {
		wait_until_lease_is_expired(lease_renewal_time)
		wait_for_active_vault_server(config.VaultUrl.Hostname())
		lease_renewal_time = ec2_auth_against_vault_server()

		log.Printf("Successfully retrieved credentials. Credentials are valid until [%s].", lease_renewal_time.Format(time.RFC1123Z))
		if !config.Agent {
			break
		}
	}
}

func init_httpClient() {
	tr := &http.Transport{
		// ignore SSL errors when talking to Vault because it could be a self-signed cert until Vault is up and running for realsies
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	timeout := time.Duration(time.Second * 10)
	client = http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
}

func wait_for_active_vault_server(server string) {
	for {
		_, err := net.LookupHost(server)
		if err != nil {
			if _, ok := err.(*net.DNSError); ok {
				log.Printf("waiting for vault server to become available at [%s]..", server)
				time.Sleep(time.Second * time.Duration(config.RetryDelay))
			} else {
				log.Fatal(err.Error())
			}
		} else {
			break
		}
	}
}

func wait_until_lease_is_expired(lease_renewal_time time.Time) {
	delay := lease_renewal_time.Sub(time.Now())
	time.Sleep(delay)
}

func ec2_auth_against_vault_server() time.Time {
	var lease_end_time time.Time
	var vault_token string
	var vault_nonce string
	var err error

	for {
		lease_end_time, vault_token, vault_nonce, err = vault_ec2_auth()
		if err != nil {
			log.Print(err.Error())
			time.Sleep(time.Second * time.Duration(config.RetryDelay))
		} else {
			break
		}
	}

	err = ioutil.WriteFile(config.TokenPath, []byte(vault_token), 0660)
	check(err)

	err = ioutil.WriteFile(config.NoncePath, []byte(vault_nonce), 0660)
	check(err)

	return get_datetime_midpoint(time.Now(), lease_end_time)
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func vault_ec2_auth() (time.Time, string, string, error) {
	pkcs7, err := get_pkcs7()
	check(err)

	nonceExists, nonce := get_nonce()

	var body []byte

	if nonceExists {
		request := ReLoginRequest{
			Role:  config.Role,
			Pkcs7: string(pkcs7),
			Nonce: nonce,
		}
		body, err = json.Marshal(request)
		check(err)
	} else {
		request := LoginRequest{
			Role:  config.Role,
			Pkcs7: string(pkcs7),
		}
		body, err = json.Marshal(request)
		check(err)
	}

	response, err := client.Post(fmt.Sprintf("%s/v1/auth/%s/login", config.VaultUrl, config.AwsMount), "application/json", bytes.NewBuffer(body))
	check(err)
	defer response.Body.Close()

	if response.StatusCode >= 300 ||
		response.StatusCode < 200 {

		b, _ := ioutil.ReadAll(response.Body)
		err := fmt.Errorf("Login attempt failed with error code [%s] - %s", response.Status, string(b))

		return time.Now(), "", "", err
	}

	result := LoginResponse{}
	err = json.NewDecoder(response.Body).Decode(&result)
	check(err)

	leaseEndTime := time.Now().Add(time.Second * time.Duration(result.Auth.LeaseDuration))

	return leaseEndTime, result.Auth.ClientToken, result.Auth.MetaData.Nonce, nil
}

func get_nonce() (bool, string) {
	if fileInfo, err := os.Stat(config.NoncePath); err == nil {
		if fileInfo.Size() > 0 {
			nonce, _ := ioutil.ReadFile(config.NoncePath)
			return true, string(nonce)
		}
	}

	return false, ""
}

func get_pkcs7() ([]byte, error) {
	ec2MetaDataEndpoint := "http://169.254.169.254/latest/dynamic/instance-identity/pkcs7"
	pkcs7Request, err := client.Get(ec2MetaDataEndpoint)
	defer pkcs7Request.Body.Close()
	pkcs7, _ := ioutil.ReadAll(pkcs7Request.Body)

	return pkcs7, err
}

func get_datetime_midpoint(time1 time.Time, time2 time.Time) time.Time {
	if time2.After(time1) {
		return time1.Add(time2.Sub(time1) / 2)
	} else {
		return time2.Add(time1.Sub(time2) / 2)
	}
}
