package store

import "testing"

func TestChannelDiagnosisGenerationOkModelsRestricted(t *testing.T) {
	diagnosis := channelDiagnosis(PublicChannel{
		Status:    "degraded",
		L1Status:  "ok",
		L2Status:  "auth_error",
		L3Status:  "ok",
		ErrorType: "models_auth_error",
	})

	if diagnosis.Code != "generation_ok_models_restricted" || diagnosis.Severity != "warn" {
		t.Fatalf("diagnosis = %+v, want generation_ok_models_restricted/warn", diagnosis)
	}
}

func TestChannelDiagnosisGenerationOkModelsSkipped(t *testing.T) {
	diagnosis := channelDiagnosis(PublicChannel{
		Status:    "healthy",
		L1Status:  "ok",
		L2Status:  "na",
		L3Status:  "ok",
		ErrorType: "",
	})

	if diagnosis.Code != "generation_ok_models_skipped" || diagnosis.Severity != "info" {
		t.Fatalf("diagnosis = %+v, want generation_ok_models_skipped/info", diagnosis)
	}
}

func TestChannelDiagnosisGenerationEmptyContent(t *testing.T) {
	diagnosis := channelDiagnosis(PublicChannel{
		Status:    "functional_down",
		L1Status:  "ok",
		L2Status:  "ok",
		L3Status:  "down",
		ErrorType: "empty_content",
	})

	if diagnosis.Code != "generation_empty_content" || diagnosis.Severity != "error" {
		t.Fatalf("diagnosis = %+v, want generation_empty_content/error", diagnosis)
	}
}
