package internal

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

const TokenEndpoint = "http://169.254.169.254/latest/api/token"
const InstanceIdEndpoint = "http://169.254.169.254/latest/meta-data/instance-id"
const SpotTerminationEndpoint = "http://169.254.169.254/latest/meta-data/spot/termination-time"

var __token string

func AreWeRunningEc2() bool {
	var someErr error
	__token, someErr = getImdsv2Token()

	return someErr == nil
}

func GetInstanceId() (string, error) {
	resp, err := getWithToken(InstanceIdEndpoint)
	if err != nil {
		return "", fmt.Errorf("getWithToken / %w", err)
	}
	defer resp.Body.Close()

	instanceId, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading instance-id / %w", err)
	}

	return string(instanceId), nil
}

func SpotTerminationIsNotified() (bool, error) {
	resp, err := getWithToken(SpotTerminationEndpoint)
	if err != nil {
		return false, fmt.Errorf("getWithToken / %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

func getWithToken(url string) (*http.Response, error) {
	requester := func(token string) (*http.Response, error) {
		client := &http.Client{Timeout: 1 * time.Second}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("NewRequest / %w", err)
		}
		req.Header.Set("X-aws-ec2-metadata-token", token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("client.Do / %w", err)
		}

		return resp, nil
	}

	// Try 1, assuming the token is valid
	resp, err := requester(__token)
	if err != nil {
		return nil, fmt.Errorf("requester / %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		__token, err = getImdsv2Token()
		if err != nil {
			return nil, fmt.Errorf("getImdsv2Token / %w", err)
		}

		// Try 2, after refreshing the token
		resp, err = requester(__token)
		if err != nil {
			return nil, fmt.Errorf("requester / %w", err)
		}
	}

	return resp, nil
}

func getImdsv2Token() (string, error) {
	client := &http.Client{Timeout: 1 * time.Second}
	req, err := http.NewRequest("PUT", TokenEndpoint, nil)
	if err != nil {
		return "", fmt.Errorf("NewRequest / %w", err)
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("client.Do / %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get token / %s", resp.StatusCode)
	}

	token, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading token / %w", err)
	}

	return string(token), nil
}
