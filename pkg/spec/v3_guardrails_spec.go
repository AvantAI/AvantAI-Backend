package spec

// // GuardrailDecision is the root object.
// type GuardrailDecision struct {
// 	Decision Decision `json:"decision"`
// 	Signals  []Signal `json:"signals"`
// }

// // -------- Decision --------

// type Decision struct {
// 	Action      DecisionAction `json:"action"`
// 	RiskLevel   RiskLevel      `json:"risk_level"`
// 	Confidence  float64        `json:"confidence"`
// 	Explanation *string        `json:"explanation,omitempty"`
// }

// type DecisionAction string

// const (
// 	ActionAllow       DecisionAction = "ALLOW"
// 	ActionSanitize    DecisionAction = "SANITIZE"
// 	ActionBlock       DecisionAction = "BLOCK"
// 	ActionHumanReview DecisionAction = "HUMAN_REVIEW"
// )

// // -------- Signals --------

// type Signal struct {
// 	Type     SignalType `json:"type"`
// 	Severity Severity   `json:"severity"`
// 	Label    string     `json:"label"`
// 	Score    float64    `json:"score"`
// }

// type SignalType string

// const (
// 	SignalIntent       SignalType = "INTENT"
// 	SignalInjection    SignalType = "INJECTION"
// 	SignalMalicious    SignalType = "MALICIOUS"
// 	SignalPII          SignalType = "PII"
// 	SignalAbuse        SignalType = "ABUSE"
// 	SignalScope        SignalType = "SCOPE"
// 	SignalPolicy       SignalType = "POLICY"
// 	SignalPurpose      SignalType = "PURPOSE"
// 	SignalAvailability SignalType = "AVAILABILITY"
// 	SignalChangeRisk   SignalType = "CHANGE_RISK"
// 	SignalOther        SignalType = "OTHER"
// )

// // -------- Shared Enums --------

// type RiskLevel string
// type Severity string

// const (
// 	LevelLow    RiskLevel = "LOW"
// 	LevelMedium RiskLevel = "MEDIUM"
// 	LevelHigh   RiskLevel = "HIGH"
// )

// const (
// 	SeverityLow    Severity = "LOW"
// 	SeverityMedium Severity = "MEDIUM"
// 	SeverityHigh   Severity = "HIGH"
// )

type Decision string

const (
	DecisionAllow       Decision = "ALLOW"
	DecisionBlock       Decision = "BLOCK"
	DecisionHumanReview Decision = "HUMAN_REVIEW"
)

type RiskLevel string

const (
	RiskLow    RiskLevel = "LOW"
	RiskMedium RiskLevel = "MEDIUM"
	RiskHigh   RiskLevel = "HIGH"
)