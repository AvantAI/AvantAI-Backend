package spec

import (
	"github.com/google/uuid"
)

type NameValueV3 struct {
	Name  string      `json:"name" yaml:"name"`
	Value interface{} `json:"value,omitempty" yaml:"value,omitempty"`
}

type NameTypeV3 struct {
	Name     string `json:"name" yaml:"name"`
	Type     string `json:"type,omitempty" yaml:"type,omitempty"`
	Format   string `json:"format,omitempty" yaml:"format,omitempty"`
	Schema   string `json:"schema,omitempty" yaml:"schema,omitempty"` // Not used now
	Default  string `json:"default,omitempty" yaml:"default,omitempty"`
	Required bool   `json:"required,omitempty" yaml:"required,omitempty"`
}

type NameTypeTextV3 struct {
	Name     string `json:"name" yaml:"name"`
	Type     string `json:"type,omitempty" yaml:"type,omitempty"`
	Format   string `json:"format,omitempty" yaml:"format,omitempty"`
	Schema   string `json:"schema,omitempty" yaml:"schema,omitempty"` // Not used now
	Default  string `json:"default,omitempty" yaml:"default,omitempty"`
	Required bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Text     string `json:"text,omitempty" yaml:"text,omitempty"`
}

type NameValueTypeV3 struct {
	Name     string      `json:"name" yaml:"name"`
	Type     string      `json:"type,omitempty" yaml:"type,omitempty"`     // text, json, file (image, audio, video, document)
	Format   string      `json:"format,omitempty" yaml:"format,omitempty"` // text/plain, application/json, text/png, ...
	Schema   string      `json:"schema,omitempty" yaml:"schema,omitempty"` // Not used, embed this info in format - URL, base64
	Default  string      `json:"default,omitempty" yaml:"default,omitempty"`
	Required bool        `json:"required,omitempty" yaml:"required,omitempty"`
	Text     string      `json:"text,omitempty" yaml:"text,omitempty"`
	Value    interface{} `json:"value,omitempty" yaml:"value,omitempty"`
}

type ProfileSpecV3 struct {
	AppID      string `json:"app_id,omitempty" yaml:"app_id,omitempty"`
	CustomerID string `json:"customer_id,omitempty" yaml:"customer_id,omitempty"`
	UserID     string `json:"user_id,omitempty" yaml:"user_id,omitempty"`
	SessionID  string `json:"session_id,omitempty" yaml:"session_id,omitempty"`
}

type ServeModelSpecV3 struct {
	ModelName       string        `json:"model_name" yaml:"model_name"`
	AccessName      string        `json:"access_name,omitempty" yaml:"access_name,omitempty"`
	AccessNamespace string        `json:"access_namespace,omitempty" yaml:"access_namespace,omitempty"`
	Params          []NameValueV3 `json:"params,omitempty" yaml:"params,omitempty"`
}

type ServeRequestSpecV3 struct {
	Profile        ProfileSpecV3     `json:"profile,omitempty" yaml:"profile,omitempty"`
	ProjectName    string            `json:"project_name,omitempty" yaml:"project_name,omitempty"`
	AgentNamespace string            `json:"agent_namespace,omitempty" yaml:"agent_namespace,omitempty"`
	AgentName      string            `json:"agent_name,omitempty" yaml:"agent_name,omitempty"`
	AgentVersion   string            `json:"agent_version,omitempty" yaml:"agent_version,omitempty"`
	Model          ServeModelSpecV3  `json:"model,omitempty" yaml:"model,omitempty"`
	Input          []NameValueTypeV3 `json:"input" yaml:"input"`
}

type ServeDebugSpecV3 struct {
	AgentNamespace string `json:"agent_namespace,omitempty" yaml:"agent_namespace,omitempty"`
	AgentName      string `json:"agent_name,omitempty" yaml:"agent_name,omitempty"`
	AgentVersion   string `json:"agent_version,omitempty" yaml:"agent_version,omitempty"`
	AppID          string `json:"app_id,omitempty" yaml:"app_id,omitempty"`
	CustomerID     string `json:"customer_id,omitempty" yaml:"customer_id,omitempty"`
	UserID         string `json:"user_id,omitempty" yaml:"user_id,omitempty"`
	SessionID      string `json:"session_id,omitempty" yaml:"session_id,omitempty"`
}

type ServeMetricsMetadata struct {
	InTokens   int   `json:"in_tokens,omitempty"`
	OutTokens  int   `json:"out_tokens,omitempty"`
	LLMLatency int64 `json:"latency,omitempty"`
	IsEstimate bool  `json:"is_estimate,omitempty"`
}

const DefaultResponseName = "_response"

type ServeResponseSpecV3 struct {
	Output   []NameValueTypeV3    `json:"output" yaml:"output"` // default response `_response`
	Matadata map[string]any       `json:"metadata" yaml:"metadata"`
	Metrics  ServeMetricsMetadata `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	Debug    ServeDebugSpecV3     `json:"debug,omitempty" yaml:"debug,omitempty"`
	ResultID string               `json:"result_id,omitempty" yaml:"result_id,omitempty"`
	// Response       string               `json:"response," yaml:"response"`
	// ResponseType   string               `json:"response_type,omitempty" yaml:"response_type,omitempty"`
	// ResponseFormat string               `json:"response_format,omitempty" yaml:"response_format,omitempty"`
	// LLMLatency     int64                `json:"latency,omitempty"`
	// InTokens       int                  `json:"in_tokens,omitempty"`
	// OutTokens      int                  `json:"out_tokens,omitempty"`
	// IsEstimate     bool                 `json:"is_estimate,omitempty"`
	// Output         []FieldValue   `json:"output,omitempty" yaml:"output,omitempty"`
}

type ServeGuardrailRequestSpecV3 struct {
	AgentName      string            `json:"agent_name" yaml:"agent_name"`
	AgentNamespace string            `json:"agent_namespace,omitempty" yaml:"agent_namespace,omitempty"`
	AgentVersion   string            `json:"agent_version,omitempty" yaml:"agent_version,omitempty"`
	Input          []NameValueTypeV3 `json:"input,omitempty" yaml:"input,omitempty"`
	Output         []NameValueTypeV3 `json:"output,omitempty" yaml:"output,omitempty"`
}

type ServeGuardrailResponseSpecV3 struct {
	// Decision Decision `json:"decision" yaml:"decision"`
	// Signals  []Signal `json:"signals,omitempty" yaml:"signals,omitempty"`
	Decision  Decision  `json:"decision"`
	Reason    string    `json:"reason"`
	RiskLevel RiskLevel `json:"risk_level"`
}

type ServeEvalRequestSpecV3 struct {
	AgentName      string            `json:"agent_name" yaml:"agent_name"`
	AgentNamespace string            `json:"agent_namespace,omitempty" yaml:"agent_namespace,omitempty"`
	AgentVersion   string            `json:"agent_version,omitempty" yaml:"agent_version,omitempty"`
	Input          []NameValueTypeV3 `json:"input,omitempty" yaml:"input,omitempty"`
	Output         []NameValueTypeV3 `json:"output,omitempty" yaml:"output,omitempty"`
}

type ServeEvalResponseSpecV3 struct {
	// Decision Decision `json:"decision" yaml:"decision"`
	// Signals  []Signal `json:"signals,omitempty" yaml:"signals,omitempty"`
	Decision  Decision  `json:"decision"`
	Reason    string    `json:"reason"`
	RiskLevel RiskLevel `json:"risk_level"`
}

func ValidateServeRequestSpecV3(reqSpec *ServeRequestSpecV3) error {
	if reqSpec.Profile.CustomerID == "" {
		reqSpec.Profile.CustomerID = uuid.Nil.String()
	}

	if reqSpec.Profile.UserID == "" {
		reqSpec.Profile.UserID = uuid.Nil.String()
	}

	if reqSpec.Profile.SessionID == "" {
		reqSpec.Profile.SessionID = uuid.Nil.String()
	}

	return nil
}
