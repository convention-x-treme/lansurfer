package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type discordTransmission uint8

const (
	discordNoFailure discordTransmission = iota
	discordBeforeTransmission
	discordTransmissionUnknown
)

// discordProviderResult never contains webhook credentials or provider body.
// A network failure is known pre-transmission only for a confirmed dial error;
// all other failures without an HTTP response may have reached Discord.
type discordProviderResult struct {
	Status      int
	DiscordCode int
	MessageID   string
	RetryAfter  *time.Duration
	Failure     discordTransmission
	Err         error
}

type discordWebhookClient struct {
	httpClient *http.Client
	baseURL    string
}

func newDiscordWebhookClient(client *http.Client, baseURL string) *discordWebhookClient {
	if client == nil {
		client = http.DefaultClient
	}
	// Never follow a provider redirect with a credential-bearing URL.
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &discordWebhookClient{httpClient: &noRedirect, baseURL: baseURL}
}

func (c *discordWebhookClient) CreateMessage(ctx context.Context, credential DiscordWebhookCredential, payload discordMessage) discordProviderResult {
	endpoint, err := c.endpoint(credential, "")
	if err != nil {
		return discordProviderResult{Failure: discordBeforeTransmission, Err: errors.New("invalid discord webhook endpoint")}
	}
	query := endpoint.Query()
	query.Set("wait", "true")
	endpoint.RawQuery = query.Encode()
	return c.message(ctx, http.MethodPost, endpoint, payload)
}

func (c *discordWebhookClient) EditMessage(ctx context.Context, credential DiscordWebhookCredential, messageID string, payload discordMessage) discordProviderResult {
	endpoint, err := c.endpoint(credential, messageID)
	if err != nil {
		return discordProviderResult{Failure: discordBeforeTransmission, Err: errors.New("invalid discord webhook endpoint")}
	}
	return c.message(ctx, http.MethodPatch, endpoint, payload)
}

func (c *discordWebhookClient) message(ctx context.Context, method string, endpoint *url.URL, payload discordMessage) discordProviderResult {
	data, err := json.Marshal(payload)
	if err != nil {
		return discordProviderResult{Failure: discordBeforeTransmission, Err: errors.New("encode discord message failed")}
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return discordProviderResult{Failure: discordBeforeTransmission, Err: errors.New("create discord webhook request failed")}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	result := c.do(request)
	if method == http.MethodPost && result.Status >= 200 && result.Status < 300 && result.Err == nil && result.MessageID == "" {
		result.Failure = discordTransmissionUnknown
		result.Err = errors.New("discord create response missing message ID")
	}
	return result
}

func (c *discordWebhookClient) DeleteWebhook(ctx context.Context, credential DiscordWebhookCredential) error {
	endpoint, err := c.endpoint(credential, "")
	if err != nil {
		return errors.New("invalid discord webhook endpoint")
	}
	endpoint.RawQuery = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint.String(), nil)
	if err != nil {
		return errors.New("create discord webhook delete request failed")
	}
	result := c.do(request)
	if result.Err != nil {
		return result.Err
	}
	if result.Status == http.StatusOK || result.Status == http.StatusNoContent || result.Status == http.StatusNotFound {
		return nil
	}
	return errors.New("delete discord webhook: status " + strconv.Itoa(result.Status))
}

func (c *discordWebhookClient) endpoint(credential DiscordWebhookCredential, suffix string) (*url.URL, error) {
	endpoint, err := url.Parse(c.baseURL)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || credential.WebhookID == "" || credential.WebhookToken == "" {
		return nil, errors.New("invalid endpoint")
	}
	// Path is decoded; RawPath preserves escaping of each credential segment.
	basePath := strings.TrimRight(endpoint.Path, "/")
	baseEscapedPath := strings.TrimRight(endpoint.EscapedPath(), "/")
	endpoint.Path = basePath + "/" + credential.WebhookID + "/" + credential.WebhookToken
	endpoint.RawPath = baseEscapedPath + "/" + url.PathEscape(credential.WebhookID) + "/" + url.PathEscape(credential.WebhookToken)
	if suffix != "" {
		endpoint.Path += "/messages/" + suffix
		endpoint.RawPath += "/messages/" + url.PathEscape(suffix)
		endpoint.RawQuery = ""
	}
	return endpoint, nil
}

func (c *discordWebhookClient) do(request *http.Request) discordProviderResult {
	response, err := c.httpClient.Do(request)
	if err != nil {
		failure := discordTransmissionUnknown
		var netErr *net.OpError
		if errors.As(err, &netErr) && netErr.Op == "dial" {
			failure = discordBeforeTransmission
		}
		return discordProviderResult{Failure: failure, Err: errors.New("discord webhook network request failed")}
	}
	defer response.Body.Close()
	result := discordProviderResult{Status: response.StatusCode}
	body, err := io.ReadAll(io.LimitReader(response.Body, discordTokenBodyLimit))
	if err != nil {
		result.Failure = discordTransmissionUnknown
		result.Err = errors.New("read discord webhook response failed")
		return result
	}
	var envelope struct {
		ID         string  `json:"id"`
		Code       int     `json:"code"`
		RetryAfter float64 `json:"retry_after"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if result.Status >= 200 && result.Status < 300 {
			result.MessageID = envelope.ID
		} else {
			result.DiscordCode = envelope.Code
		}
	}
	if result.Status == http.StatusTooManyRequests {
		if value := strings.TrimSpace(response.Header.Get("Retry-After")); value != "" {
			result.RetryAfter = discordRetrySeconds(value)
			if result.RetryAfter == nil {
				if date, err := http.ParseTime(value); err == nil {
					delay := time.Until(date)
					result.RetryAfter = &delay
				}
			}
		}
		if result.RetryAfter == nil && envelope.RetryAfter > 0 && envelope.RetryAfter <= float64(math.MaxInt64)/float64(time.Second) {
			delay := time.Duration(envelope.RetryAfter * float64(time.Second))
			result.RetryAfter = &delay
		}
	}
	return result
}

func discordRetrySeconds(value string) *time.Duration {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds < 0 || seconds > float64(math.MaxInt64)/float64(time.Second) {
		return nil
	}
	delay := time.Duration(seconds * float64(time.Second))
	return &delay
}
