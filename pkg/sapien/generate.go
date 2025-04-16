package sapien

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

type AgentRequest struct {
	Input   []Field `json:"input" yaml:"input"`
	Profile Profile `json:"profile,omitempty" yaml:"profile,omitempty"`
}

type AgentDebug struct {
	AgentNamespace string `json:"agent_namespace,omitempty" yaml:"agent_namespace,omitempty"`
	AgentName      string `json:"agent_name,omitempty" yaml:"agent_name,omitempty"`
	AgentVersion   string `json:"agent_version,omitempty" yaml:"agent_version,omitempty"`
	AppID          string `json:"app_id,omitempty" yaml:"app_id,omitempty"`
	CustomerID     string `json:"customer_id,omitempty" yaml:"customer_id,omitempty"`
	UserID         string `json:"user_id,omitempty" yaml:"user_id,omitempty"`
	SessionID      string `json:"session_id,omitempty" yaml:"session_id,omitempty"`
}

type AgentResponse struct {
	Output []Field    `json:"output" yaml:"output"`
	Debug  AgentDebug `json:"debug,omitempty" yaml:"debug,omitempty"`
}

type ServeDebug struct {
	AgentNamespace string `json:"agent_namespace,omitempty" yaml:"agent_namespace,omitempty"`
	AgentName      string `json:"agent_name,omitempty" yaml:"agent_name,omitempty"`
	AgentVersion   string `json:"agent_version,omitempty" yaml:"agent_version,omitempty"`
	AppID          string `json:"app_id,omitempty" yaml:"app_id,omitempty"`
	CustomerID     string `json:"customer_id,omitempty" yaml:"customer_id,omitempty"`
	UserID         string `json:"user_id,omitempty" yaml:"user_id,omitempty"`
	SessionID      string `json:"session_id,omitempty" yaml:"session_id,omitempty"`
}
type ServeResponse struct {
	Response       string     `json:"response" yaml:"response"`
	ResponseType   string     `json:"response_type,omitempty" yaml:"response_type,omitempty"`
	ResponseFormat string     `json:"response_format,omitempty" yaml:"response_format,omitempty"`
	Debug          ServeDebug `json:"debug,omitempty" yaml:"debug,omitempty"`
}

type Profile struct {
	AppID      string `json:"app_id,omitempty" yaml:"app_id,omitempty"`
	CustomerID string `json:"customer_id,omitempty" yaml:"customer_id,omitempty"`
	UserID     string `json:"user_id,omitempty" yaml:"user_id,omitempty"`
	SessionID  string `json:"session_id,omitempty" yaml:"session_id,omitempty"`
}

type Field struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
}

type ServeRequest struct {
	Profile Profile `json:"profile,omitempty" yaml:"profile,omitempty"`
	Input   []Field `json:"input" yaml:"input"`
}

type LLMInput struct {
}

type LLMOutput struct {
}

type LLMError struct {
}

type SapienApi struct {
	Host   string
	ApiKey string
	Logger *zap.Logger
}

func NewSapienApi(host string, apiKey string, logger *zap.Logger) *SapienApi {
	return &SapienApi{
		Host:   host,
		ApiKey: apiKey,
		Logger: logger,
	}
}

func (s *SapienApi) ServeReqUrl(agentNamespace string, agentName string, version string) string {
	params := ""
	if version != "" {
		params = "?version=" + url.QueryEscape(version)
	}
	return s.Host + "/serve/v1/agents/generate/" + url.PathEscape(agentNamespace) + "/" + url.PathEscape(agentName) + params
}

func (s *SapienApi) GenerateCompletion(agentNamespace string, agentName string, serverReq *ServeRequest) (int, string, *ServeResponse, error) {

	apiUrl := s.ServeReqUrl(agentNamespace, agentName, "")

	reqBody, err := json.Marshal(serverReq)
	if err != nil {
		fmt.Printf("Could not read the response body %s\n", err.Error())
		return 500, "InternalError", nil, err
	}

	req, err := http.NewRequest("POST", apiUrl, bytes.NewReader(reqBody))
	if err != nil {
		fmt.Println(err.Error())
		return 500, "InternalError", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.ApiKey)

	fmt.Println("POST", apiUrl)
	fmt.Println("Bearer " + s.ApiKey)

	client := http.Client{Timeout: 300 * time.Second}

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err.Error())
		return 500, "InternalError", nil, err
	}
	fmt.Printf("%s\n---\n", res.Status)

	defer res.Body.Close()
	if res.StatusCode == 200 {
		resp := &ServeResponse{}

		err = json.NewDecoder(res.Body).Decode(resp)
		if err != nil {
			fmt.Println(err.Error())
			return 500, "InternalError", nil, err
		}

		return res.StatusCode, res.Status, resp, nil
	}
	return res.StatusCode, res.Status, nil, nil
}
