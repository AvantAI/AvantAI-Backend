package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/kaptinlin/jsonrepair"
	"go.uber.org/zap"
)

const DefaultNamespace string = "avant"
const DefaultSapienUrl string = "http://localhost:4081"

type HTTPError struct {
	StatusCode int
	Status     string
	Err        error
}

func NewHTTPError(statusCode int, err error) *HTTPError {
	return &HTTPError{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Err:        err,
	}
}

func (e *HTTPError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Status
}

func (e *HTTPError) Unwrap() error {
	return e.Err
}

type SapienConfig struct {
	ApiUrl    string
	ApiKey    string
	Namespace string
	Timeout   time.Duration
}

type SapienClient struct {
	Config *SapienConfig
	Client *http.Client
	Logger *zap.Logger
}

func NewSapienClient(config *SapienConfig, logger *zap.Logger) *SapienClient {
	client := &http.Client{Timeout: 300 * time.Second}
	return &SapienClient{
		Config: config,
		Client: client,
		Logger: logger,
	}
}

func (s *SapienClient) Generate(agentName string, agentNamespace string, serverReq *ServeRequestSpecV3) (*ServeResponseSpecV3, *HTTPError) {

	if agentNamespace == "" {
		agentNamespace = s.Config.Namespace
	}

	//serveUrl := s.Config.ApiUrl + "/serve/v3/agents/generate/" + url.PathEscape(agentNamespace) + "/" + url.PathEscape(agentName)
	serveUrl := s.Config.ApiUrl + "/serve/v3/runs/agents"
	fmt.Println("s.Generate serveUrl=", serveUrl)
	reqBody, err := json.Marshal(serverReq)
	if err != nil {
		fmt.Printf("Could not read the response body %s\n", err.Error())
		return nil, NewHTTPError(http.StatusInternalServerError, err)
	}

	req, err := http.NewRequest("POST", serveUrl, bytes.NewReader(reqBody))
	if err != nil {
		fmt.Println(err.Error())
		return nil, NewHTTPError(http.StatusInternalServerError, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.Config.ApiKey)

	fmt.Println("Authorization:", "Bearer "+s.Config.ApiKey)

	//client := http.Client{Timeout: 300 * time.Second}

	res, err := s.Client.Do(req)
	if err != nil {
		fmt.Println(err.Error())
		return nil, NewHTTPError(http.StatusInternalServerError, err)
	}
	fmt.Printf("%s\n---\n", res.Status)

	defer res.Body.Close()
	if res.StatusCode == 200 {
		resp := []ServeResponseSpecV3{}

		err = json.NewDecoder(res.Body).Decode(&resp)
		if err != nil {
			fmt.Println(err.Error())
			return nil, NewHTTPError(http.StatusInternalServerError, err)
		}

		return &resp[0], nil
	}
	return nil, NewHTTPError(res.StatusCode, err)
}

func Generate(agentName string, serverReq *ServeRequestSpecV3, jsonResp bool, logger *zap.Logger) (string, error) {

	sapienConfig := &SapienConfig{
		ApiKey:    os.Getenv("SAPIEN_TOKEN"),
		ApiUrl:    "http://localhost:4081",
		Namespace: "avant",
	}
	//fmt.Println("Generate=", sapienConfig.ApiUrl, sapienConfig.Namespace, sapienConfig.ApiKey)

	if sapienConfig.ApiKey == "" {
		return "", fmt.Errorf("missing api key")
	}

	if sapienConfig.ApiUrl == "" {
		sapienConfig.ApiUrl = DefaultSapienUrl
	}

	if sapienConfig.Namespace == "" {
		sapienConfig.Namespace = DefaultNamespace
	}

	sapienClient := NewSapienClient(sapienConfig, logger)

	resp, httpErr := sapienClient.Generate(agentName, "", serverReq)

	if httpErr != nil {
		fmt.Printf("StatusCode: %d status: %s err: %s", httpErr.StatusCode, httpErr.Status, httpErr.Err)
		return "", fmt.Errorf("%s", httpErr.Status)
	}

	var err error
	response := resp.Output[0].Value.(string)
	if jsonResp {
		response, err = jsonrepair.JSONRepair(response)
		if err != nil {
			fmt.Println("Error repairing JSON:", err)
			return "", err
		}
	}

	return response, nil
}
