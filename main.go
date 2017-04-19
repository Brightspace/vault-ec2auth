package main

import (
	"net"
	"log"
	"time"
	"io/ioutil"
	"path/filepath"
	"os/user"
	"net/http"
	"encoding/json"
	"crypto/tls"
	"bytes"
	"net/url"
	"fmt"
	"os"
	"flag"
)

type Config struct {
	Role      string
	VaultUrl  *url.URL
	TokenPath string
	NoncePath string
}

type SealStatus struct {
	Sealed      bool `json:"sealed"`
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
		Renewable     bool `json:"renewable"`
		LeaseDuration int32 `json:"lease_duration"`
		MetaData struct {
			RoleTagMaxTtl string `json:"role_tag_max_ttl"`
			Role          string `json:"role"`
			Region        string `json:"region"`
			Nonce         string `json:"nonce"`
			InstanceId    string `json:"instance_id"`
			AmiId         string `json:"ami_id"`
		} `json:"metadata"`
		Policies    []string `json:"policies"`
		Accessor    string `json:"accessor"`
		ClientToken string `json:"client_token"`
	} `json:"auth"`
	Warnings []string `json:"warnings"`
	WrapInfo struct {
		TTL             time.Duration `json:"ttl"`
		Token           string `json:"token"`
		CreationTime    time.Time `json:"creation_time"`
		WrappedAccessor string `json:"wrapped_accessor"`
		Format          string `json:"format"`
	} `json:"wrap_info"`
	LeaseDuration int32 `json:"lease_duration"`
	Renewable     bool `json:"renewable"`
	LeaseId       string `json:"lease_id"`
	RequestId     string `json:"request_id"`
}

var client http.Client
var config Config

func init() {
	var err error
	usr, _ := user.Current()
	var vaultUrlParameter string

	flag.StringVar(&vaultUrlParameter, "vault-url", "https://vault.service.consul:8200", "the full url to the vault node to auth against")
	config.VaultUrl, err = url.Parse(vaultUrlParameter)
	check(err)

	flag.StringVar(&config.Role, "role", "", "the vault role to request")

	flag.StringVar(&config.NoncePath, "nonce-path", filepath.Join(usr.HomeDir, ".vault-nonce"), "the path to the nonce file")
	flag.StringVar(&config.TokenPath, "token-path", filepath.Join(usr.HomeDir, ".vault-token"), "the path to the token file")

	flag.Parse()
	if config.Role == "" {
		flag.Usage()
		log.Fatal("Must provide a role to auth with.")
	}

	init_httpClient()
}

func main() {
	lease_renewal_time := time.Now()

	for {
		wait_until_lease_is_expired(lease_renewal_time)
		wait_for_active_vault_server(config.VaultUrl.Hostname())
		lease_renewal_time = ec2_auth_against_vault_server()
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
				log.Printf("waiting for vault server to be available at [%s]", server)
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
	log.Printf("sleeping for %f seconds", delay.Seconds())
	time.Sleep(delay)
}

func ec2_auth_against_vault_server() time.Time {
	lease_end_time, vault_token, vault_nonce := vault_ec2_auth()

	err := ioutil.WriteFile(config.TokenPath, []byte(vault_token), 0660)
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

func vault_ec2_auth() (time.Time, string, string) {
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

	log.Println(fmt.Sprintf("%s/v1/auth/aws-ec2/login", config.VaultUrl))

	response, err := client.Post(fmt.Sprintf("%s/v1/auth/aws-ec2/login", config.VaultUrl), "application/json", bytes.NewBuffer(body))
	check(err)
	defer response.Body.Close()


	if response.StatusCode >= 300 ||
		response.StatusCode < 200 {

		b, _ := ioutil.ReadAll(response.Body)
		log.Fatalf("Login attempt failed with error code [%d %s]\n%s", response.StatusCode, response.Status, string(b))
	}

	result := LoginResponse{}
	err = json.NewDecoder(response.Body).Decode(&result)
	check(err)
	log.Printf("%+v\n", result)

	leaseEndTime := time.Now().Add(time.Second * time.Duration(result.Auth.LeaseDuration))

	return leaseEndTime, result.Auth.ClientToken, result.Auth.MetaData.Nonce
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
