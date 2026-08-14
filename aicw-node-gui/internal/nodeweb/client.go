package nodeweb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

type Eligibility struct {
	RegisteredNodeCount int     `json:"registeredNodeCount"`
	RequiredStakeSol    float64 `json:"requiredStakeSol"`
	CanRegister         bool    `json:"canRegister"`
	BlockReason         *string `json:"blockReason"`
}

type NodeRecord struct {
	NodeID    string  `json:"nodeId"`
	NodeName  *string `json:"nodeName"`
	PublicKey *string `json:"publicKey"`
	Status    string  `json:"status"`
}

type GuiURLs struct {
	RecommendedAction string `json:"recommendedAction"`
	CanLaunchNode     bool   `json:"canLaunchNode"`
	StakingURL        string `json:"stakingUrl"`
	DashboardURL      string `json:"dashboardUrl"`
	RegisterURL       string `json:"registerUrl"`
	OnboardingURL     string `json:"onboardingUrl"`
}

type WalletStatus struct {
	Wallet      string      `json:"wallet"`
	Eligibility Eligibility `json:"eligibility"`
	Nodes       []NodeRecord `json:"nodes"`
	GUI         GuiURLs     `json:"gui"`
}

type VerifyResponse struct {
	Wallet   string `json:"wallet"`
	Verified bool   `json:"verified"`
}

func (c *Client) GetWalletStatus(wallet string) (*WalletStatus, error) {
	endpoint := fmt.Sprintf("%s/api/gui/status?wallet=%s", c.BaseURL, url.QueryEscape(wallet))
	resp, err := c.HTTPClient.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var status WalletStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *Client) VerifyLogin(challengeToken, wallet, signatureBase64, message string) (*VerifyResponse, error) {
	payload := map[string]string{
		"challengeToken":  challengeToken,
		"wallet":          wallet,
		"signatureBase64": signatureBase64,
		"message":         message,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Post(
		c.BaseURL+"/api/auth/verify",
		"application/json",
		strings.NewReader(string(raw)),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("verify failed: %s", strings.TrimSpace(string(body)))
	}

	var out VerifyResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func AuthGUIURL(baseURL, callbackURL string) string {
	return fmt.Sprintf("%s/auth/gui?callback=%s", strings.TrimRight(baseURL, "/"), url.QueryEscape(callbackURL))
}

func AuthRegisterURL(baseURL, callbackURL, nodeID, nodeName, publicKey string) string {
	params := url.Values{}
	params.Set("callback", callbackURL)
	params.Set("purpose", "register")
	params.Set("nodeId", nodeID)
	if strings.TrimSpace(nodeName) != "" {
		params.Set("nodeName", nodeName)
	}
	if strings.TrimSpace(publicKey) != "" {
		params.Set("publicKey", publicKey)
	}
	return fmt.Sprintf("%s/auth/gui?%s", strings.TrimRight(baseURL, "/"), params.Encode())
}

func AuthActionURL(baseURL, callbackURL, purpose, nodeID, nodeName string) string {
	params := url.Values{}
	params.Set("callback", callbackURL)
	params.Set("purpose", purpose)
	if strings.TrimSpace(nodeID) != "" {
		params.Set("nodeId", nodeID)
	}
	if strings.TrimSpace(nodeName) != "" {
		params.Set("nodeName", nodeName)
	}
	return fmt.Sprintf("%s/auth/gui?%s", strings.TrimRight(baseURL, "/"), params.Encode())
}

type OnboardingConfig struct {
	NodeWebURL          string `json:"nodeWebUrl"`
	PingIntervalSeconds int    `json:"pingIntervalSeconds"`
	ReleasesURL         string `json:"releasesUrl"`
	NetworkConfigYaml   string `json:"networkConfigYaml"`
}

func (c *Client) GetOnboardingConfig() (*OnboardingConfig, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/api/onboarding/config")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("onboarding config %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out OnboardingConfig
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type RegisterNodeRequest struct {
	NodeID              string `json:"nodeId"`
	NodeName            string `json:"nodeName,omitempty"`
	PublicKey           string `json:"publicKey,omitempty"`
	ChallengeToken      string `json:"challengeToken"`
	Wallet              string `json:"wallet"`
	SignatureBase64     string `json:"signatureBase64"`
	Message             string `json:"message"`
	SignedMessageBase64 string `json:"signedMessageBase64,omitempty"`
}

type RegisterNodeResponse struct {
	Node struct {
		NodeID   string `json:"nodeId"`
		NodeName string `json:"nodeName"`
	} `json:"node"`
}

func (c *Client) RegisterNode(req RegisterNodeRequest) (*RegisterNodeResponse, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Post(
		c.BaseURL+"/api/nodes",
		"application/json",
		strings.NewReader(string(raw)),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("register node failed: %s", strings.TrimSpace(string(body)))
	}

	var out RegisterNodeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type ActiveNodesResponse struct {
	Active   []string `json:"active"`
	MaxAgeMs int      `json:"maxAgeMs"`
}

func (c *Client) GetActiveNodeIDs() (map[string]bool, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/api/nodes/active")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("active nodes %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out ActiveNodesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	active := make(map[string]bool, len(out.Active))
	for _, id := range out.Active {
		active[id] = true
	}
	return active, nil
}

func (c *Client) WaitForNodeInactive(nodeID string, timeout time.Duration) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("node ID is required")
	}

	deadline := time.Now().Add(timeout)
	for {
		active, err := c.GetActiveNodeIDs()
		if err != nil {
			return err
		}
		if !active[nodeID] {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"node still active on the network after stop; wait a few minutes and try again",
			)
		}
		time.Sleep(10 * time.Second)
	}
}

func NodeRecordName(node NodeRecord) string {
	if node.NodeName != nil && strings.TrimSpace(*node.NodeName) != "" {
		return strings.TrimSpace(*node.NodeName)
	}
	return ""
}

type OffboardNodeRequest struct {
	Wallet              string `json:"wallet"`
	NodeID              string `json:"nodeId"`
	NodeName            string `json:"nodeName,omitempty"`
	ChallengeToken      string `json:"challengeToken"`
	SignatureBase64     string `json:"signatureBase64"`
	Message             string `json:"message"`
	SignedMessageBase64 string `json:"signedMessageBase64,omitempty"`
}

type OffboardNodeResponse struct {
	Phase             string  `json:"phase"`
	Wallet            string  `json:"wallet"`
	NodeID            string  `json:"nodeId"`
	NodeName          *string `json:"nodeName"`
	RemainingNodes    int     `json:"remainingNodes"`
	ReturnAvailableAt *string `json:"returnAvailableAt"`
	Message           string  `json:"message"`
}

func (c *Client) OffboardNode(req OffboardNodeRequest) (*OffboardNodeResponse, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Post(
		c.BaseURL+"/api/offboard/node",
		"application/json",
		strings.NewReader(string(raw)),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("offboard node failed: %s", strings.TrimSpace(string(body)))
	}

	var out OffboardNodeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type OffboardStatusResponse struct {
	Wallet               string  `json:"wallet"`
	RegisteredNodeCount  int     `json:"registeredNodeCount"`
	ReturnAvailableAt    *string `json:"returnAvailableAt"`
	HoursUntilReturn     *float64 `json:"hoursUntilReturn"`
	IsReturnDue          bool    `json:"isReturnDue"`
	PendingUnstake       *struct {
		Status            string  `json:"status"`
		AmountSol         float64 `json:"amountSol"`
		ReturnAvailableAt *string `json:"returnAvailableAt"`
	} `json:"pendingUnstake"`
}

func (c *Client) GetOffboardStatus(wallet string) (*OffboardStatusResponse, error) {
	endpoint := fmt.Sprintf("%s/api/offboard/status?wallet=%s", c.BaseURL, url.QueryEscape(wallet))
	resp, err := c.HTTPClient.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("offboard status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out OffboardStatusResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) LogLocalIdentityRemoved(wallet, nodeID, nodeName string) error {
	payload := map[string]string{
		"wallet":   wallet,
		"nodeId":   nodeID,
		"nodeName": nodeName,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Post(
		c.BaseURL+"/api/offboard/local-removed",
		"application/json",
		strings.NewReader(string(raw)),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("log local removed failed: %s", strings.TrimSpace(string(body)))
	}
	return nil
}
