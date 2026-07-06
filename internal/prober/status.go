package prober

type LayerSummary struct {
	Status    string
	LatencyMs int
	ErrorType string
}

type StatusDecision struct {
	Status    string
	ErrorType string
}

func SynthesizeStatus(l1 LayerSummary, l2 LayerSummary) StatusDecision {
	return SynthesizeStatusWithL3(l1, l2, LayerSummary{Status: "na"})
}

func SynthesizeStatusWithL3(l1 LayerSummary, l2 LayerSummary, l3 LayerSummary) StatusDecision {
	if l3.Status != "" && l3.Status != "na" {
		if l3.Status == "auth_error" || l3.ErrorType == "auth_error" {
			return StatusDecision{Status: "auth_error", ErrorType: "auth_error"}
		}
		if l3.Status == "ok" {
			if l1.Status == "down" {
				return StatusDecision{Status: "degraded", ErrorType: firstNonEmpty(l1.ErrorType, "connectivity_down")}
			}
			if l2.Status != "" && l2.Status != "na" && l2.Status != "ok" {
				return StatusDecision{Status: "degraded", ErrorType: modelsProbeErrorType(l2)}
			}
			return StatusDecision{Status: "healthy"}
		}
		if l3.Status == "warn" || l3.ErrorType == "rate_limited" || l3.ErrorType == "slow_response" {
			return StatusDecision{Status: "degraded", ErrorType: firstNonEmpty(l3.ErrorType, "slow_response")}
		}
		if l3.Status == "down" {
			if l2.ErrorType == "model_not_found" || l2.ErrorType == "model_unavailable" {
				return StatusDecision{Status: "functional_down", ErrorType: l2.ErrorType}
			}
			return StatusDecision{Status: "functional_down", ErrorType: firstNonEmpty(l3.ErrorType, "empty_content")}
		}
	}
	if l1.Status == "down" {
		if l2.Status == "ok" {
			return StatusDecision{Status: "degraded", ErrorType: firstNonEmpty(l1.ErrorType, "connectivity_down")}
		}
		return StatusDecision{Status: "connectivity_down", ErrorType: firstNonEmpty(l1.ErrorType, "connectivity_down")}
	}
	if l1.Status == "" || l1.Status == "na" || l2.Status == "" || l2.Status == "na" {
		return StatusDecision{Status: "unknown", ErrorType: "unknown"}
	}
	if l2.Status == "auth_error" || l2.ErrorType == "auth_error" {
		return StatusDecision{Status: "auth_error", ErrorType: "auth_error"}
	}
	if l2.Status == "down" {
		return StatusDecision{Status: "connectivity_down", ErrorType: firstNonEmpty(l2.ErrorType, "connectivity_down")}
	}
	if l1.Status == "ok" && l2.Status == "ok" {
		return StatusDecision{Status: "healthy"}
	}
	return StatusDecision{Status: "unknown", ErrorType: firstNonEmpty(l1.ErrorType, l2.ErrorType, "unknown")}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func modelsProbeErrorType(l2 LayerSummary) string {
	if l2.Status == "auth_error" || l2.ErrorType == "auth_error" {
		return "models_auth_error"
	}
	switch l2.ErrorType {
	case "model_not_found", "model_unavailable":
		return l2.ErrorType
	case "timeout":
		return "models_timeout"
	case "rate_limited":
		return "models_rate_limited"
	case "l2_probe_skipped", "models_probe_skipped":
		return "models_probe_skipped"
	case "":
		return "models_unavailable"
	default:
		return "models_unavailable"
	}
}
