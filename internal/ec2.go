package internal

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const TokenEndpoint = "http://169.254.169.254/latest/api/token"
const SpotTerminationEndpoint = "http://169.254.169.254/latest/meta-data/spot/termination-time"

var __token string

func AreWeRunningEc2() (inEc2 bool, err error) {
	var someErr error
	__token, someErr = getImdsv2Token()

	if someErr != nil {
		var netErr net.Error
		if errors.As(someErr, &netErr) && netErr.Timeout() {
			inEc2 = false
			err = nil
		} else {
			err = someErr
		}
	} else {
		inEc2 = true
	}
	return
}

func SpotTerminationIsNotified() (bool, error) {
	requester := func(token string) (int, error) {
		client := &http.Client{Timeout: 1 * time.Second}
		req, err := http.NewRequest("GET", SpotTerminationEndpoint, nil)
		if err != nil {
			return 0, fmt.Errorf("NewRequest / %w", err)
		}
		req.Header.Set("X-aws-ec2-metadata-token", token)

		resp, err := client.Do(req)
		if err != nil {
			return 0, fmt.Errorf("client.Do / %w", err)
		}
		defer resp.Body.Close()

		return resp.StatusCode, nil
	}

	// Try 1, assuming the token is valid
	statusCode, err := requester(__token)
	if err != nil {
		return false, fmt.Errorf("requester / %w", err)
	}

	if statusCode == http.StatusUnauthorized {
		__token, err = getImdsv2Token()
		if err != nil {
			return false, fmt.Errorf("getImdsv2Token / %w", err)
		}

		// Try 2, after refreshing the token
		statusCode, err = requester(__token)
		if err != nil {
			return false, fmt.Errorf("requester / %w", err)
		}
	}

	return statusCode == http.StatusOK, nil
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
